package database

import (
	"database/sql"
	"fmt"
	"time"
)

// 本文件：定时任务调度框架（ScheduledTask / ScheduledTaskLog / ScheduledTaskAudit）相关表与 CRUD 操作。

// ==================== Scheduled Task ====================

// ScheduledTask 任务定义。
// TaskType 取值：built_in（系统内置） | business（用户业务）。
type ScheduledTask struct {
	ID             int64  `json:"id"`
	TaskKey        string `json:"task_key"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	TaskType       string `json:"task_type"`
	CronExpr       string `json:"cron_expr"`
	HandlerName    string `json:"handler_name"`
	HandlerParams  string `json:"handler_params"`
	IsActive       bool   `json:"is_active"`
	IsConcurrent   bool   `json:"is_concurrent"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	LastRunAt      int64  `json:"last_run_at"`
	LastStatus     string `json:"last_status"`
	LastError      string `json:"last_error"`
	NextRunAt      int64  `json:"next_run_at"`
	RunCount       int64  `json:"run_count"`
	FailCount      int64  `json:"fail_count"`
	CreatedBy      int64  `json:"created_by"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// scheduledTaskCols 是查询 scheduled_task 全部字段的列名。
const scheduledTaskCols = `
	id, task_key, name, description, task_type, cron_expr,
	handler_name, handler_params, is_active, is_concurrent, timeout_seconds,
	last_run_at, last_status, last_error, next_run_at, run_count, fail_count,
	created_by, created_at, updated_at
`

// CreateScheduledTask 插入一条任务定义。
func CreateScheduledTask(t *ScheduledTask) (int64, error) {
	return createScheduledTaskWith(DB, t)
}

// CreateScheduledTaskTx 在指定事务中插入任务定义；tx 为 nil 时回退到非事务路径。
func CreateScheduledTaskTx(tx *sql.Tx, t *ScheduledTask) (int64, error) {
	if tx == nil {
		return CreateScheduledTask(t)
	}
	return createScheduledTaskWith(tx, t)
}

// sqlExecutor 是 *sql.DB 和 *sql.Tx 的共同接口（都提供 Exec），用于让
// createScheduledTaskWith 同时支持事务和非事务路径，避免代码重复。
type sqlExecutor interface {
	Exec(query string, args ...interface{}) (sql.Result, error)
}

// createScheduledTaskWith 实际执行 INSERT 的内部函数。
func createScheduledTaskWith(exec sqlExecutor, t *ScheduledTask) (int64, error) {
	now := time.Now().Unix()
	if t.CreatedAt == "" {
		t.CreatedAt = fmt.Sprintf("%d", now)
	}
	if t.UpdatedAt == "" {
		t.UpdatedAt = fmt.Sprintf("%d", now)
	}
	if t.LastStatus == "" {
		t.LastStatus = "pending"
	}
	if t.HandlerParams == "" {
		t.HandlerParams = "{}"
	}
	if t.TimeoutSeconds == 0 {
		t.TimeoutSeconds = 3600
	}
	result, err := exec.Exec(`
		INSERT INTO scheduled_task (
			task_key, name, description, task_type, cron_expr,
			handler_name, handler_params, is_active, is_concurrent, timeout_seconds,
			last_run_at, last_status, last_error, next_run_at, run_count, fail_count,
			created_by, created_at, updated_at
		) VALUES (?,?,?,?,?, ?,?,?,?,?, ?,?,?,?,?,?, ?,?,?)
	`,
		t.TaskKey, t.Name, t.Description, t.TaskType, t.CronExpr,
		t.HandlerName, t.HandlerParams, boolToInt(t.IsActive), boolToInt(t.IsConcurrent), t.TimeoutSeconds,
		0, t.LastStatus, "", 0, 0, 0,
		t.CreatedBy, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetScheduledTaskByID 按 id 查询任务定义。
func GetScheduledTaskByID(id int64) (*ScheduledTask, error) {
	t := &ScheduledTask{}
	row := DB.QueryRow(`SELECT `+scheduledTaskCols+` FROM scheduled_task WHERE id = ?`, id)
	if err := scanScheduledTask(row, t); err != nil {
		return nil, err
	}
	return t, nil
}

// GetScheduledTaskByKey 按 task_key 查询（用于启动时种子数据幂等判断）；未找到返回 (nil, nil)。
func GetScheduledTaskByKey(taskKey string) (*ScheduledTask, error) {
	t := &ScheduledTask{}
	row := DB.QueryRow(`SELECT `+scheduledTaskCols+` FROM scheduled_task WHERE task_key = ?`, taskKey)
	if err := scanScheduledTask(row, t); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return t, nil
}

// GetAllActiveScheduledTasks 拉取 is_active=1 的全部任务（启动时加载到调度器）。
func GetAllActiveScheduledTasks() ([]ScheduledTask, error) {
	rows, err := DB.Query(`SELECT ` + scheduledTaskCols + ` FROM scheduled_task WHERE is_active = 1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []ScheduledTask
	for rows.Next() {
		var t ScheduledTask
		if err := scanScheduledTask(rows, &t); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	if tasks == nil {
		tasks = []ScheduledTask{}
	}
	return tasks, rows.Err()
}

// GetAllScheduledTasks 拉取全部任务（不分页），给管理页用。
func GetAllScheduledTasks() ([]ScheduledTask, error) {
	rows, err := DB.Query(`SELECT ` + scheduledTaskCols + ` FROM scheduled_task ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []ScheduledTask
	for rows.Next() {
		var t ScheduledTask
		if err := scanScheduledTask(rows, &t); err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	if tasks == nil {
		tasks = []ScheduledTask{}
	}
	return tasks, rows.Err()
}

// UpdateScheduledTask 更新任务定义（不可改 task_key / task_type）。
func UpdateScheduledTask(t *ScheduledTask) error {
	_, err := DB.Exec(`
		UPDATE scheduled_task SET
			name = ?, description = ?, cron_expr = ?,
			handler_name = ?, handler_params = ?,
			is_active = ?, is_concurrent = ?, timeout_seconds = ?,
			updated_at = ?
		WHERE id = ?
	`,
		t.Name, t.Description, t.CronExpr,
		t.HandlerName, t.HandlerParams,
		boolToInt(t.IsActive), boolToInt(t.IsConcurrent), t.TimeoutSeconds,
		time.Now().Unix(), t.ID,
	)
	return err
}

// UpdateScheduledTaskActive 启停某个任务。
func UpdateScheduledTaskActive(id int64, isActive bool) error {
	_, err := DB.Exec(`UPDATE scheduled_task SET is_active = ?, updated_at = ? WHERE id = ?`,
		boolToInt(isActive), time.Now().Unix(), id)
	return err
}

// UpdateScheduledTaskRunResult 任务执行后写回结果（last_run_at / last_status / last_error / next_run_at / run_count / fail_count）。
//
// 仅在任务**实际执行后**调用。
// success=true → run_count+1
// success=false → fail_count+1
func UpdateScheduledTaskRunResult(id int64, lastRunAt, nextRunAt int64, status, errMsg string, success bool) error {
	sqlStr := `UPDATE scheduled_task SET last_run_at = ?, last_status = ?, last_error = ?, next_run_at = ?, updated_at = ?`
	args := []interface{}{lastRunAt, status, errMsg, nextRunAt, time.Now().Unix()}
	if success {
		sqlStr += `, run_count = run_count + 1`
	} else {
		sqlStr += `, fail_count = fail_count + 1`
	}
	sqlStr += ` WHERE id = ?`
	args = append(args, id)
	_, err := DB.Exec(sqlStr, args...)
	return err
}

// UpdateScheduledTaskNextRun 仅更新 next_run_at（**不触碰** run_count / fail_count / last_run_at）。
//
// 用途：调度器 reload（启动加载、配置变更）时计算下次执行时间。
// ⚠️ 修复 Bug：原 UpdateScheduledTaskRunResult(..., false) 会被 reload 调用，
// 导致 fail_count 持续增长（即使任务未执行）。现在拆开。
func UpdateScheduledTaskNextRun(id int64, nextRunAt int64) error {
	_, err := DB.Exec(`
		UPDATE scheduled_task
		SET next_run_at = ?, updated_at = ?
		WHERE id = ?
	`, nextRunAt, time.Now().Unix(), id)
	return err
}

// DeleteScheduledTask 删除任务（仅允许非内置任务，由 handler 校验）。
func DeleteScheduledTask(id int64) error {
	_, err := DB.Exec(`DELETE FROM scheduled_task WHERE id = ?`, id)
	return err
}

// ==================== Scheduled Task Log ====================

// ScheduledTaskLog 任务执行日志。
// Status 取值：running / success / failed / timeout / skipped。
type ScheduledTaskLog struct {
	ID          int64  `json:"id"`
	TaskID      int64  `json:"task_id"`
	TaskKey     string `json:"task_key"`
	StartedAt   int64  `json:"started_at"`
	FinishedAt  int64  `json:"finished_at"`
	DurationMs  int64  `json:"duration_ms"`
	Status      string `json:"status"`
	Output      string `json:"output"`
	Error       string `json:"error"`
	TriggeredBy string `json:"triggered_by"`
	OperatorID  int64  `json:"operator_id"`
}

// scheduledTaskLogCols 是查询 scheduled_task_log 全部字段的列名。
const scheduledTaskLogCols = `
	id, task_id, task_key, started_at, finished_at, duration_ms,
	status, output, error, triggered_by, operator_id
`

// CreateScheduledTaskLog 写入执行日志（status=running 时调用）。
func CreateScheduledTaskLog(log *ScheduledTaskLog) (int64, error) {
	result, err := DB.Exec(`
		INSERT INTO scheduled_task_log (task_id, task_key, started_at, status, triggered_by, operator_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, log.TaskID, log.TaskKey, log.StartedAt, log.Status, log.TriggeredBy, log.OperatorID)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// FinishScheduledTaskLog 任务结束时更新日志（写回 finished_at / duration_ms / status / output / error）。
func FinishScheduledTaskLog(id int64, finishedAt, durationMs int64, status, output, errMsg string) error {
	_, err := DB.Exec(`
		UPDATE scheduled_task_log
		SET finished_at = ?, duration_ms = ?, status = ?, output = ?, error = ?
		WHERE id = ?
	`, finishedAt, durationMs, status, output, errMsg, id)
	return err
}

// GetScheduledTaskLogs 分页查询某任务的执行日志。
func GetScheduledTaskLogs(taskID int64, page, pageSize int) ([]ScheduledTaskLog, int, error) {
	var total int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM scheduled_task_log WHERE task_id = ?`, taskID).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	rows, err := DB.Query(`SELECT `+scheduledTaskLogCols+`
		FROM scheduled_task_log WHERE task_id = ? ORDER BY started_at DESC LIMIT ? OFFSET ?`,
		taskID, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var logs []ScheduledTaskLog
	for rows.Next() {
		var l ScheduledTaskLog
		if err := rows.Scan(
			&l.ID, &l.TaskID, &l.TaskKey, &l.StartedAt, &l.FinishedAt, &l.DurationMs,
			&l.Status, &l.Output, &l.Error, &l.TriggeredBy, &l.OperatorID,
		); err != nil {
			return nil, 0, err
		}
		logs = append(logs, l)
	}
	if logs == nil {
		logs = []ScheduledTaskLog{}
	}
	return logs, total, rows.Err()
}

// CleanupOldScheduledTaskLogs 仅保留某任务的最近 retention 条日志，多余的删掉。
func CleanupOldScheduledTaskLogs(retention int) (int64, error) {
	res, err := DB.Exec(`
		DELETE FROM scheduled_task_log
		WHERE id NOT IN (
			SELECT id FROM scheduled_task_log
			ORDER BY started_at DESC LIMIT ?
		)
	`, retention)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ==================== Helpers ====================

// rowScanner 让 QueryRow 与 Query 都能复用同一套 Scan 逻辑。
type rowScanner interface {
	Scan(dest ...interface{}) error
}

// scanScheduledTask 把单行扫描到 ScheduledTask，同时把 INTEGER 0/1 反序列化为 bool、可空字符串反序列化为 string。
func scanScheduledTask(s rowScanner, t *ScheduledTask) error {
	var (
		isActive, isConcurrent int
		description            sql.NullString
		handlerParams          sql.NullString
		lastStatus             sql.NullString
		lastError              sql.NullString
	)
	err := s.Scan(
		&t.ID, &t.TaskKey, &t.Name, &description, &t.TaskType, &t.CronExpr,
		&t.HandlerName, &handlerParams, &isActive, &isConcurrent, &t.TimeoutSeconds,
		&t.LastRunAt, &lastStatus, &lastError, &t.NextRunAt, &t.RunCount, &t.FailCount,
		&t.CreatedBy, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return err
	}
	t.IsActive = isActive != 0
	t.IsConcurrent = isConcurrent != 0
	if description.Valid {
		t.Description = description.String
	}
	if handlerParams.Valid {
		t.HandlerParams = handlerParams.String
	}
	if lastStatus.Valid {
		t.LastStatus = lastStatus.String
	}
	if lastError.Valid {
		t.LastError = lastError.String
	}
	return nil
}

// boolToInt 将 bool 转为 0/1（SQLite 没有原生 BOOLEAN，列定义是 INTEGER）。
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ==================== Scheduled Task Audit ====================

// ScheduledTaskAudit 任务变更审计记录。
// Action 取值：update_cron / update_handler / update_params / enable / disable / create / delete。
type ScheduledTaskAudit struct {
	ID           int64  `json:"id"`
	TaskID       int64  `json:"task_id"`
	TaskKey      string `json:"task_key"`
	Action       string `json:"action"`
	FieldName    string `json:"field_name"`
	OldValue     string `json:"old_value"`
	NewValue     string `json:"new_value"`
	OperatorID   int64  `json:"operator_id"`
	OperatorName string `json:"operator_name"`
	CreatedAt    string `json:"created_at"`
}

// CreateScheduledTaskAudit 写一条审计记录。
func CreateScheduledTaskAudit(a *ScheduledTaskAudit) (int64, error) {
	result, err := DB.Exec(`
		INSERT INTO scheduled_task_audit
			(task_id, task_key, action, field_name, old_value, new_value, operator_id, operator_name)
		VALUES (?,?,?,?,?,?,?,?)
	`, a.TaskID, a.TaskKey, a.Action, a.FieldName, a.OldValue, a.NewValue, a.OperatorID, a.OperatorName)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// GetScheduledTaskAudits 分页查询某任务的审计记录。
func GetScheduledTaskAudits(taskID int64, page, pageSize int) ([]ScheduledTaskAudit, int, error) {
	var total int
	if err := DB.QueryRow(`SELECT COUNT(*) FROM scheduled_task_audit WHERE task_id = ?`, taskID).Scan(&total); err != nil {
		return nil, 0, err
	}
	offset := (page - 1) * pageSize
	rows, err := DB.Query(`
		SELECT id, task_id, task_key, action, field_name, old_value, new_value,
		       operator_id, operator_name, created_at
		FROM scheduled_task_audit
		WHERE task_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, taskID, pageSize, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var audits []ScheduledTaskAudit
	for rows.Next() {
		var a ScheduledTaskAudit
		if err := rows.Scan(
			&a.ID, &a.TaskID, &a.TaskKey, &a.Action, &a.FieldName,
			&a.OldValue, &a.NewValue, &a.OperatorID, &a.OperatorName, &a.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		audits = append(audits, a)
	}
	if audits == nil {
		audits = []ScheduledTaskAudit{}
	}
	return audits, total, rows.Err()
}

// CleanupOldScheduledTaskAudits 仅保留某任务的最近 retention 条审计记录。
func CleanupOldScheduledTaskAudits(retention int) (int64, error) {
	res, err := DB.Exec(`
		DELETE FROM scheduled_task_audit
		WHERE id NOT IN (
			SELECT id FROM scheduled_task_audit
			ORDER BY created_at DESC LIMIT ?
		)
	`, retention)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
