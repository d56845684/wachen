# wachen AWS IaC — 對映 docs/architecture-aws.png
#
# 用法（各環境同一套 code，換 tfvars + state key）：
#   terraform init -backend-config="bucket=<state-bucket>" -backend-config="key=wachen/dev.tfstate"
#   terraform plan -var-file=envs/dev.tfvars
#   terraform apply -var-file=envs/dev.tfvars

terraform {
  required_version = ">= 1.6"
  required_providers {
    aws    = { source = "hashicorp/aws", version = "~> 5.0" }
    random = { source = "hashicorp/random", version = "~> 3.0" }
  }
  backend "s3" {
    region       = "ap-northeast-1"
    use_lockfile = true
  }
}

provider "aws" {
  region = var.region
  default_tags {
    tags = { Project = "wachen", Env = var.env, ManagedBy = "terraform" }
  }
}

locals {
  name = "wachen-${var.env}"
}
