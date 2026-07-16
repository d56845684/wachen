# 事件層：NATS JetStream topic → SQS queue（每條佇列帶 DLQ）
locals {
  queues = [
    "crawl-jobs",
    "review-raw",
    "review-created",
    "review-analyzed",
    "case-created",
    "reply-requested",
    "reply-result",
  ]
}

resource "aws_sqs_queue" "dlq" {
  for_each                  = toset(local.queues)
  name                      = "${local.name}-${each.key}-dlq"
  message_retention_seconds = 1209600 # 14 天，人工檢視
}

resource "aws_sqs_queue" "main" {
  for_each                   = toset(local.queues)
  name                       = "${local.name}-${each.key}"
  visibility_timeout_seconds = 300
  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq[each.key].arn
    maxReceiveCount     = 4 # 必須 = queue.MaxDeliver（backend/internal/queue）
  })
}
