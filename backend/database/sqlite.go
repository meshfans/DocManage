package database

// 本文件原包含所有数据库表和 CRUD（> 2900 行）。
// 已按业务域拆分到以下文件：
//   - db.go              DB 连接、迁移、建表、建索引、种子用户
//   - user.go            User 表（含扩展表 UserWithDepartment）
//   - message.go         Message 通知
//   - customer.go        Customer + SignatureRecord
//   - template.go        Template 模板
//   - form_field.go      FormField 表单字段定义
//   - flow.go            FlowTemplate + FlowStep（工作流定义）
//   - department.go      Department 部门
//   - system_config.go   SystemConfig + EnsureUpdatedAtProgress
//   - dashboard.go       DashboardStats
//   - scheduled_task.go  ScheduledTask + ScheduledTaskLog + ScheduledTaskAudit
//
// 保留本文件以维持 Go 包的最小文件数要求。
