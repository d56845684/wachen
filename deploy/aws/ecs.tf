# 三個 image：backend（全部 Go binary，command 選服務）、analyzer（Python）、web（nginx+SPA）
resource "aws_ecr_repository" "repo" {
  for_each = toset(["backend", "analyzer", "web"])
  name     = "wachen/${each.key}"
}

resource "aws_ecs_cluster" "main" {
  name = local.name
}

resource "aws_cloudwatch_log_group" "svc" {
  for_each          = var.services
  name              = "/ecs/${local.name}/${each.key}"
  retention_in_days = var.env == "prod" ? 90 : 14
}

locals {
  image_of = {
    for k in keys(var.services) : k => (
      k == "web" ? "${aws_ecr_repository.repo["web"].repository_url}:${var.image_tag}" :
      k == "analyzer" ? "${aws_ecr_repository.repo["analyzer"].repository_url}:${var.image_tag}" :
      "${aws_ecr_repository.repo["backend"].repository_url}:${var.image_tag}"
    )
  }
  common_env = concat(
    [
      { name = "ENV", value = var.env },
      { name = "AWS_REGION", value = var.region },
      { name = "QUEUE_DRIVER", value = "sqs" },
    ],
    [for q in local.queues : { name = "SQS_${replace(upper(q), "-", "_")}_URL", value = aws_sqs_queue.main[q].url }],
  )
}

resource "aws_ecs_task_definition" "svc" {
  for_each                 = var.services
  family                   = "${local.name}-${each.key}"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc"
  cpu                      = each.value.cpu
  memory                   = each.value.memory
  execution_role_arn       = aws_iam_role.execution.arn
  task_role_arn            = each.key == "analyzer" ? aws_iam_role.analyzer.arn : aws_iam_role.task.arn

  container_definitions = jsonencode([{
    name         = each.key
    image        = local.image_of[each.key]
    command      = contains(["web", "analyzer"], each.key) ? null : [each.key]
    portMappings = each.value.port > 0 ? [{ containerPort = each.value.port }] : []
    environment  = local.common_env
    secrets = [
      { name = "DB_SECRET_JSON", valueFrom = aws_secretsmanager_secret.db.arn },
    ]
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.svc[each.key].name
        "awslogs-region"        = var.region
        "awslogs-stream-prefix" = each.key
      }
    }
  }])
}

resource "aws_ecs_service" "svc" {
  for_each        = var.services
  name            = each.key
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.svc[each.key].arn
  desired_count   = each.value.count
  launch_type     = "FARGATE"

  network_configuration {
    subnets         = module.vpc.private_subnets
    security_groups = [aws_security_group.app.id]
  }

  dynamic "load_balancer" {
    for_each = each.value.port > 0 ? [1] : []
    content {
      target_group_arn = aws_lb_target_group.svc[each.key].arn
      container_name   = each.key
      container_port   = each.value.port
    }
  }
}
