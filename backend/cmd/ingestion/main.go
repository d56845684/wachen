// Ingestion Service：review.raw → 正規化 → reviews（upsert）→ review.created。
//
//	raw_reviews（版本化 append-only）      reviews（一則評論一列）
//	  v1 hash-a ──┐
//	  v2 hash-b ──┼── 取事件對應版本 ──▶ upsert by (source, external_id)
//	  v3 hash-c ──┘                      │ 新版本 → 更新內容 + status='new'
//	                                     │          （audit 留痕、觸發重新分析）
//	                                     │ 過時/重放版本 → 略過（防回捲）
//	                                     ▼
//	                              review.created → M4 Analysis
//
// 冪等：同一 review.raw 事件重放 → upsert 判定同版本 → 不重發 review.created。
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/ikala/wachen/backend/internal/bootstrap"
	"github.com/ikala/wachen/backend/internal/normalize"
	"github.com/ikala/wachen/backend/internal/store"
)

// 依賴以小介面注入，錯誤路徑可用 fake 測試（同 worker 的 4A 模式）
type ingestStore interface {
	GetRawForIngest(ctx context.Context, rawReviewID string) (*store.RawForIngest, error)
	UpsertReview(ctx context.Context, p store.UpsertReviewParams) (string, store.UpsertOutcome, error)
	FindUnreflectedRaws(ctx context.Context, olderThan time.Duration, limit int) ([]string, error)
	FindStaleNewReviews(ctx context.Context, olderThan time.Duration, limit int) ([]string, error)
	QuarantineRaw(ctx context.Context, rawReviewID, reason string) error
}

type createdPublisher interface {
	PublishReviewCreated(ctx context.Context, reviewID string) error
}

func main() {
	svc := bootstrap.MustInit("ingestion", "svc:ingestion")
	defer svc.Close()
	ctx, log, st, q := svc.Ctx, svc.Log, svc.Store, svc.Queue

	cc, err := q.ConsumeReviewRaw(ctx, func(ctx context.Context, rawID string, attempt uint64, isFinal bool) error {
		return ingestOne(ctx, log, st, q, rawID, attempt, isFinal)
	})
	if err != nil {
		log.Error("consume failed", "err", err)
		os.Exit(1)
	}
	defer cc.Stop()

	// 對帳掃描：事件流的最後一道網，60s 一輪、兩條腿
	//   腿 1：最新 raw 未反映到 reviews（死信/漏 review.raw/亂序殘留）→ 重跑 ingest
	//   腿 2：status='new' 卡超過 15min（review.created 發佈重試耗盡）→ 重發事件
	go func() {
		t := time.NewTicker(60 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			// 每輪上限刻意小（兩腿共用 20）：backlog 高峰時事件仍在佇列中排隊，
			// 大量重發只會變成重複事件產生器（下游冪等吸收，但白燒 LLM 配額）
			reingested, republished, err := reconcileOnce(ctx, log, st, q, 2*time.Minute, 15*time.Minute, 20)
			if err != nil {
				log.Error("reconciliation pass failed", "err", err)
			} else if reingested > 0 || republished > 0 {
				log.Warn("reconciliation recovered", "reingested", reingested, "republished", republished)
			}
		}
	}()

	log.Info("ingestion started")
	<-ctx.Done()
	log.Info("shutting down")
}

// reconcileOnce 執行一輪雙腿對帳（冪等）。單筆失敗不中斷整輪。
func reconcileOnce(ctx context.Context, log *slog.Logger, st ingestStore, pub createdPublisher,
	rawOlderThan, staleNewOlderThan time.Duration, limit int) (reingested, republished int, err error) {

	rawIDs, err := st.FindUnreflectedRaws(ctx, rawOlderThan, limit)
	if err != nil {
		return 0, 0, err
	}
	for _, id := range rawIDs {
		log.Warn("reconciliation re-ingesting", "raw_review_id", id)
		if err := ingestOne(ctx, log, st, pub, id, 1, false); err != nil {
			log.Error("reconcile ingest failed, will retry next pass", "raw_review_id", id, "err", err)
			continue
		}
		reingested++
	}

	reviewIDs, err := st.FindStaleNewReviews(ctx, staleNewOlderThan, limit)
	if err != nil {
		return reingested, 0, err
	}
	for _, id := range reviewIDs {
		if err := pub.PublishReviewCreated(ctx, id); err != nil {
			log.Error("reconcile republish failed", "review_id", id, "err", err)
			continue
		}
		republished++
	}
	return reingested, republished, nil
}

func ingestOne(ctx context.Context, log *slog.Logger, st ingestStore, pub createdPublisher,
	rawID string, attempt uint64, isFinal bool) error {

	raw, err := st.GetRawForIngest(ctx, rawID)
	if err != nil {
		// raw 尚未可見（極罕見的重放競態）→ 重試；耗盡由佇列 Term
		log.Error("load raw failed", "raw_review_id", rawID, "attempt", attempt, "err", err)
		return err
	}
	n, err := normalize.Normalize(raw.Adapter, raw.Payload)
	if err != nil {
		// 格式壞掉重試也不會好：進隔離區（否則對帳掃描每輪重撿 = 無限迴圈），
		// 人工修復後刪 ingest_quarantine 列即可重入管線
		log.Error("normalize failed, quarantining", "raw_review_id", rawID, "adapter", raw.Adapter, "err", err)
		if qErr := st.QuarantineRaw(ctx, rawID, err.Error()); qErr != nil {
			log.Error("quarantine failed", "raw_review_id", rawID, "err", qErr)
			return qErr // 隔離失敗必須重試，不能讓毒藥 raw 消失無蹤
		}
		return nil
	}
	reviewID, outcome, err := st.UpsertReview(ctx, store.UpsertReviewParams{
		RawReviewID: raw.ID,
		SourceName:  raw.SourceName,
		ExternalID:  raw.ExternalID,
		AuthorName:  n.AuthorName,
		Rating:      n.Rating,
		Content:     n.Content,
		PostedAt:    n.PostedAt,
		SourceURL:   raw.SourceURL,
		LocationID:  raw.LocationID,
	})
	if err != nil {
		if isFinal {
			log.Error("upsert dead-lettered", "raw_review_id", rawID, "err", err)
		}
		return fmt.Errorf("upsert review: %w", err)
	}
	switch outcome {
	case store.UpsertStale, store.UpsertDeleted, store.UpsertPointerOnly:
		// Stale=亂序舊版本；Deleted=軟刪除不復活；
		// PointerOnly=顧客內容未變（商家回覆等）——三者都不觸發下游
		log.Info("no analysis trigger", "raw_review_id", rawID, "review_id", reviewID, "outcome", outcome.String())
		return nil
	}
	// Applied 與 Replay 都要發：Replay = 上次 publish 失敗後的重試，
	// 下游（M4）以 review_id + 內容 hash 冪等，重複事件無害
	if err := pub.PublishReviewCreated(ctx, reviewID); err != nil {
		log.Error("publish review.created failed", "review_id", reviewID, "err", err)
		return err // 任務失敗 → 佇列重試；重試耗盡由對帳第二條腿（stale-new）兜底
	}
	log.Info("review ingested", "review_id", reviewID, "source", raw.SourceName,
		"external_id", raw.ExternalID, "outcome", outcome, "attempt", attempt)
	return nil
}
