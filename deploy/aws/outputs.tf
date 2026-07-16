output "app_url" {
  value = "https://${var.domain}"
}

output "alb_dns" {
  value = aws_lb.main.dns_name
}

output "db_endpoint" {
  value = aws_db_instance.pg.address
}

output "db_secret_arn" {
  value = aws_secretsmanager_secret.db.arn
}

output "ecr_repos" {
  value = { for k, r in aws_ecr_repository.repo : k => r.repository_url }
}

output "queue_urls" {
  value = { for k, q in aws_sqs_queue.main : k => q.url }
}
