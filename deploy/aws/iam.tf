data "aws_iam_policy_document" "ecs_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ecs-tasks.amazonaws.com"]
    }
  }
}

data "aws_iam_policy_document" "ec2_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

# 拉 image + 讀 secrets + 寫 log（ECS 啟動用）
resource "aws_iam_role" "execution" {
  name               = "${local.name}-execution"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

resource "aws_iam_role_policy_attachment" "execution" {
  role       = aws_iam_role.execution.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonECSTaskExecutionRolePolicy"
}

resource "aws_iam_role_policy" "execution_secrets" {
  name = "secrets"
  role = aws_iam_role.execution.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["secretsmanager:GetSecretValue"]
      Resource = [aws_secretsmanager_secret.db.arn]
    }]
  })
}

# 應用共用權限：SQS + DB secret
data "aws_iam_policy_document" "app" {
  statement {
    actions = [
      "sqs:SendMessage", "sqs:ReceiveMessage", "sqs:DeleteMessage",
      "sqs:GetQueueAttributes", "sqs:GetQueueUrl",
    ]
    resources = concat(
      [for q in aws_sqs_queue.main : q.arn],
      [for q in aws_sqs_queue.dlq : q.arn],
    )
  }
  statement {
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [aws_secretsmanager_secret.db.arn]
  }
}

resource "aws_iam_role" "task" {
  name               = "${local.name}-task"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

resource "aws_iam_role_policy" "task_app" {
  name   = "app"
  role   = aws_iam_role.task.id
  policy = data.aws_iam_policy_document.app.json
}

# analyzer 獨立 role：只有它能打 Bedrock
resource "aws_iam_role" "analyzer" {
  name               = "${local.name}-analyzer"
  assume_role_policy = data.aws_iam_policy_document.ecs_assume.json
}

resource "aws_iam_role_policy" "analyzer_app" {
  name   = "app"
  role   = aws_iam_role.analyzer.id
  policy = data.aws_iam_policy_document.app.json
}

resource "aws_iam_role_policy" "analyzer_bedrock" {
  name = "bedrock"
  role = aws_iam_role.analyzer.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["bedrock:InvokeModel"]
      Resource = "*" # ponytail: 選定模型後收斂到該 model ARN
    }]
  })
}

# EC2 爬蟲/回覆 worker
resource "aws_iam_role" "crawler" {
  name               = "${local.name}-crawler"
  assume_role_policy = data.aws_iam_policy_document.ec2_assume.json
}

resource "aws_iam_role_policy" "crawler_app" {
  name   = "app"
  role   = aws_iam_role.crawler.id
  policy = data.aws_iam_policy_document.app.json
}

resource "aws_iam_role_policy_attachment" "crawler_ecr" {
  role       = aws_iam_role.crawler.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonEC2ContainerRegistryReadOnly"
}

resource "aws_iam_role_policy_attachment" "crawler_ssm" {
  role       = aws_iam_role.crawler.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore" # 免 SSH，用 Session Manager
}

resource "aws_iam_instance_profile" "crawler" {
  name = "${local.name}-crawler"
  role = aws_iam_role.crawler.name
}
