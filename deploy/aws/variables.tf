variable "env" {
  type        = string
  description = "dev / stg / prod"
}

variable "region" {
  type    = string
  default = "ap-northeast-1"
}

variable "vpc_cidr" {
  type    = string
  default = "10.0.0.0/16"
}

variable "domain" {
  type        = string
  description = "後台網域，如 wachen-dev.example.com"
}

variable "hosted_zone_id" {
  type        = string
  description = "Route 53 hosted zone（自家 DNS）"
}

variable "image_tag" {
  type    = string
  default = "latest"
}

variable "db_instance_class" {
  type    = string
  default = "db.t4g.micro"
}

variable "db_allocated_storage" {
  type    = number
  default = 20
}

variable "crawler_instance_type" {
  type    = string
  default = "t4g.small"
}

variable "crawler_count" {
  type    = number
  default = 2 # 架構驗證點：2+ replicas 分食任務
}

# worker Spot 候選機型（皆 arm64 配合 AMI）；機型越多中斷風險越分散
variable "crawler_spot_instance_types" {
  type    = list(string)
  default = ["t4g.small", "t4g.medium", "c6g.medium"]
}

# ECS Fargate 服務。port=0 = 純 worker（不掛 ALB）。
# scheduler 跑常駐服務：PG advisory lock 選主已內建，比 EventBridge 定時喚醒省事。
variable "services" {
  type = map(object({
    cpu    = number
    memory = number
    port   = number
    count  = number
  }))
  default = {
    web       = { cpu = 256, memory = 512, port = 80, count = 1 }
    api       = { cpu = 256, memory = 512, port = 8080, count = 1 }
    webhook   = { cpu = 256, memory = 512, port = 8080, count = 1 }
    scheduler = { cpu = 256, memory = 512, port = 0, count = 1 }
    ingestion = { cpu = 256, memory = 512, port = 0, count = 1 }
    routing   = { cpu = 256, memory = 512, port = 0, count = 1 }
    analyzer  = { cpu = 512, memory = 1024, port = 0, count = 1 }
  }
}
