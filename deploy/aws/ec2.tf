# 爬蟲 + 留言回覆：長駐輪詢、共用平台憑證 → EC2 ASG（對映架構圖的 VM）
data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]
  filter {
    name   = "name"
    values = ["al2023-ami-*-arm64"]
  }
}

locals {
  crawler_roles = ["worker", "replier"]
  crawler_user_data = {
    for role in local.crawler_roles : role => base64encode(<<-EOF
      #!/bin/bash
      dnf install -y docker && systemctl enable --now docker
      aws ecr get-login-password --region ${var.region} \
        | docker login --username AWS --password-stdin ${aws_ecr_repository.repo["backend"].repository_url}
      docker run -d --restart always --name ${role} \
        -e ENV=${var.env} -e AWS_REGION=${var.region} -e QUEUE_DRIVER=sqs \
        -e DB_SECRET_ARN=${aws_secretsmanager_secret.db.arn} \
        %{for q in local.queues~}
        -e SQS_${replace(upper(q), "-", "_")}_URL=${aws_sqs_queue.main[q].url} \
        %{endfor~}
        ${aws_ecr_repository.repo["backend"].repository_url}:${var.image_tag} ${role}
    EOF
    )
  }
}

resource "aws_launch_template" "crawler" {
  for_each      = toset(local.crawler_roles)
  name_prefix   = "${local.name}-${each.key}-"
  image_id      = data.aws_ami.al2023.id
  instance_type = var.crawler_instance_type
  user_data     = local.crawler_user_data[each.key]

  iam_instance_profile {
    name = aws_iam_instance_profile.crawler.name
  }
  vpc_security_group_ids = [aws_security_group.app.id]
}

# worker 全 Spot：任務冪等 + MQ 未 ack 重投 + reaper 回收孤兒任務，中斷 = 已設計過的 worker 崩潰。
# replier 對外發文（雖有 idempotency_key 保護），保守留 on-demand。
resource "aws_autoscaling_group" "crawler" {
  for_each            = toset(local.crawler_roles)
  name                = "${local.name}-${each.key}"
  desired_capacity    = each.key == "worker" ? var.crawler_count : 1
  min_size            = 1
  max_size            = each.key == "worker" ? var.crawler_count * 2 : 2
  vpc_zone_identifier = module.vpc.private_subnets

  # Spot 容量吃緊前主動換機（收到 rebalance 建議即補新機再收舊機）
  capacity_rebalance = each.key == "worker"

  dynamic "launch_template" {
    for_each = each.key == "worker" ? [] : [1]
    content {
      id      = aws_launch_template.crawler[each.key].id
      version = "$Latest"
    }
  }

  dynamic "mixed_instances_policy" {
    for_each = each.key == "worker" ? [1] : []
    content {
      instances_distribution {
        on_demand_base_capacity                  = 0
        on_demand_percentage_above_base_capacity = 0
        spot_allocation_strategy                 = "price-capacity-optimized"
      }
      launch_template {
        launch_template_specification {
          launch_template_id = aws_launch_template.crawler["worker"].id
          version            = "$Latest"
        }
        # 多機型分散中斷風險（都是 arm64，配合 AMI）
        dynamic "override" {
          for_each = var.crawler_spot_instance_types
          content {
            instance_type = override.value
          }
        }
      }
    }
  }

  tag {
    key                 = "Name"
    value               = "${local.name}-${each.key}"
    propagate_at_launch = true
  }
  # ponytail: 固定台數；要依佇列深度擴縮時再加 target tracking policy（SQS depth metric）
}
