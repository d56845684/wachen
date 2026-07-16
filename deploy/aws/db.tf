resource "random_password" "db" {
  length  = 32
  special = false
}

resource "aws_secretsmanager_secret" "db" {
  name_prefix = "${local.name}-db-"
}

resource "aws_secretsmanager_secret_version" "db" {
  secret_id = aws_secretsmanager_secret.db.id
  secret_string = jsonencode({
    username = "wachen"
    password = random_password.db.result
    host     = aws_db_instance.pg.address
    port     = 5432
    dbname   = "wachen"
  })
}

resource "aws_db_subnet_group" "pg" {
  name       = local.name
  subnet_ids = module.vpc.private_subnets
}

resource "aws_db_instance" "pg" {
  identifier        = local.name
  engine            = "postgres"
  engine_version    = "16"
  instance_class    = var.db_instance_class
  allocated_storage = var.db_allocated_storage

  db_name  = "wachen"
  username = "wachen"
  password = random_password.db.result

  db_subnet_group_name   = aws_db_subnet_group.pg.name
  vpc_security_group_ids = [aws_security_group.db.id]

  multi_az                = var.env == "prod"
  backup_retention_period = var.env == "prod" ? 7 : 1
  skip_final_snapshot     = var.env != "prod"
  deletion_protection     = var.env == "prod"
  storage_encrypted       = true
}
