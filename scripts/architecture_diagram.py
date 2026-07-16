#!/usr/bin/env python3
"""產生系統架構圖（GCP + AWS 兩版）：輸出 docs/architecture-gcp.png、docs/architecture-aws.png。

執行：uv run --with diagrams scripts/architecture_diagram.py
需要 graphviz（brew install graphviz）。

服務對映（PoC docker-compose → 雲端正式環境）：
  排程器          Cloud Scheduler      EventBridge Scheduler
  爬蟲/留言回覆    GCE VM               EC2（長駐輪詢、共用平台憑證）
  其餘服務        Cloud Run            ECS Fargate
  事件層          Pub/Sub              SQS（PoC: NATS JetStream）
  資料庫          Cloud SQL            RDS（PostgreSQL）
  LLM            Vertex AI Gemini     Bedrock
  對外入口        Cloud DNS + LB       Route 53 + ELB（PoC: Cloudflare Tunnel）
"""
import pathlib

from diagrams import Cluster, Diagram, Edge
from diagrams.aws.compute import EC2, ECS
from diagrams.aws.database import RDS
from diagrams.aws.integration import SQS, Eventbridge
from diagrams.aws.network import ELB, Route53
from diagrams.gcp.analytics import PubSub
from diagrams.gcp.compute import ComputeEngine, Run
from diagrams.gcp.database import SQL
from diagrams.gcp.devtools import Scheduler
from diagrams.gcp.ml import AIPlatform
from diagrams.gcp.network import DNS, LoadBalancing
from diagrams.generic.device import Mobile
from diagrams.onprem.client import Users
from diagrams.onprem.network import Internet
from diagrams.saas.chat import Line
from diagrams.saas.social import Facebook

try:  # ponytail: 舊版 diagrams 沒有 Bedrock，退 Sagemaker 圖示
    from diagrams.aws.ml import Bedrock as AwsLLM
except ImportError:
    from diagrams.aws.ml import Sagemaker as AwsLLM

DOCS = pathlib.Path(__file__).resolve().parent.parent / "docs"

PROVIDERS = {
    "gcp": dict(svc=Run, vm=ComputeEngine, sched=Scheduler, mq=PubSub,
                db=SQL, llm=AIPlatform, dns=DNS, lb=LoadBalancing,
                llm_label="LLM (Gemini)"),
    "aws": dict(svc=ECS, vm=EC2, sched=Eventbridge, mq=SQS,
                db=RDS, llm=AwsLLM, dns=Route53, lb=ELB,
                llm_label="LLM (Bedrock)"),
}


def build(provider: str, n: dict) -> pathlib.Path:
    out = DOCS / f"architecture-{provider}"
    with Diagram(
        f"顧客負評追蹤系統（{provider.upper()}）",
        filename=str(out),
        show=False,
        direction="LR",
    ):
        with Cluster("資料來源"):
            google = Internet("Google 評論")
            social = Facebook("FB / IG")
            app = Mobile("官網 / APP")
            cs = Line("客服 LINE / Email")

        with Cluster("負評收集"):
            sched = n["sched"]("排程器")
            worker = n["vm"]("爬蟲 VM x N")
            webhook = n["svc"]("推送接收")
            replier = n["vm"]("留言回覆")

        mq = n["mq"]("事件層")

        with Cluster("案件處理"):
            ingestion = n["svc"]("資料清洗")
            routing = n["svc"]("分流指派")
            api = n["svc"]("API 服務")

        with Cluster("AI 分析"):
            analyzer = n["svc"]("輿情分析")
            gemini = n["llm"](n["llm_label"])

        db = n["db"]("資料庫\n(含稽核軌跡)")

        with Cluster("後台"):
            dns = n["dns"]("DNS")
            lb = n["lb"]("負載平衡")
            web = n["svc"]("管理介面")
            user = Users("使用者")

        # 收集
        google >> worker
        social >> worker
        app >> webhook
        cs >> webhook
        sched >> Edge(label="派工") >> mq >> worker
        worker >> Edge(label="原始留言") >> mq
        webhook >> Edge(label="原始留言") >> mq

        # 清洗 → 分析 → 分流
        mq >> ingestion >> db
        ingestion >> Edge(label="新留言") >> mq >> analyzer
        analyzer >> gemini
        analyzer >> db
        analyzer >> Edge(label="分析完成") >> mq >> routing
        routing >> db
        routing >> Edge(label="Email / LINE 通知", style="dashed") >> user

        # 回覆鏈路
        api >> Edge(label="回覆請求") >> mq >> replier
        replier >> Edge(label="平台 API 回覆") >> google

        # 後台與資料
        api >> db
        user >> dns >> lb >> web >> api

    return out.with_suffix(".png")


for provider, nodes in PROVIDERS.items():
    png = build(provider, nodes)
    assert png.exists() and png.stat().st_size > 10_000, f"{png} 沒產出來"
    print(f"OK: {png} ({png.stat().st_size} bytes)")
