env            = "prod"
domain         = "wachen.example.com" # TODO: 換自家網域
hosted_zone_id = "ZXXXXXXXXXXXXX"     # TODO: 換自家 hosted zone

vpc_cidr             = "10.1.0.0/16"
db_instance_class    = "db.r6g.large"
db_allocated_storage = 100
crawler_count        = 3

services = {
  web       = { cpu = 256, memory = 512, port = 80, count = 2 }
  api       = { cpu = 512, memory = 1024, port = 8080, count = 2 }
  webhook   = { cpu = 256, memory = 512, port = 8080, count = 2 }
  scheduler = { cpu = 256, memory = 512, port = 0, count = 1 } # 選主機制內建，多開也安全但沒必要
  ingestion = { cpu = 512, memory = 1024, port = 0, count = 2 }
  routing   = { cpu = 512, memory = 1024, port = 0, count = 2 }
  analyzer  = { cpu = 1024, memory = 2048, port = 0, count = 2 }
}
