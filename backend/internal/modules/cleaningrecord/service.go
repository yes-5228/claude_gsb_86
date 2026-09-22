package cleaningrecord

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/drainage/desilting/internal/httpx"
	"github.com/drainage/desilting/internal/modules/cleaningtask"
	"github.com/drainage/desilting/internal/shared/date"
	"github.com/drainage/desilting/internal/shared/option"
	"github.com/drainage/desilting/internal/shared/refx"
	"github.com/drainage/desilting/internal/shared/sludge"
)

// TaskGateway 清淤任务模块对外提供的能力（由 cleaningtask.Service 实现）。
type TaskGateway interface {
	FindByID(ctx context.Context, id uint) (*cleaningtask.CleaningTask, error)
	EnsureStarted(ctx context.Context, id uint) (*cleaningtask.CleaningTask, error)
}

// SludgeGateway 清淤量换算能力（由 conversion.Service 实现）。
type SludgeGateway interface {
	// StandardT 按清淤日期当时生效的规则把原始值折算为干重（干污泥 t）。
	StandardT(ctx context.Context, amount float64, caliber string, cleanedAt date.Date) (float64, error)
}

// Service 清淤记录业务逻辑。
type Service struct {
	repo   *Repository
	tasks  TaskGateway
	sludge SludgeGateway
}

// NewService 构造服务。
func NewService(repo *Repository, tasks TaskGateway, sludge SludgeGateway) *Service {
	return &Service{repo: repo, tasks: tasks, sludge: sludge}
}

// decorate 为单条记录填充折算干重。
func (s *Service) decorate(ctx context.Context, record *CleaningRecord) error {
	standard, err := s.sludge.StandardT(ctx, record.SludgeAmount, record.SludgeCaliber, record.CleanedAt)
	if err != nil {
		return httpx.WrapInternal("折算清淤量失败", err)
	}
	record.StandardT = standard
	return nil
}

// decorateAll 批量填充折算干重。
func (s *Service) decorateAll(ctx context.Context, records []CleaningRecord) error {
	for i := range records {
		if err := s.decorate(ctx, &records[i]); err != nil {
			return err
		}
	}
	return nil
}

// Create 录入清淤记录。首次录入时会把任务从"待开工"推进到"清淤中"。
func (s *Service) Create(ctx context.Context, req SaveRequest) (*CleaningRecord, error) {
	if err := validate(req); err != nil {
		return nil, err
	}
	// 先确认任务可录入（同时完成状态推进），避免写入脏数据。
	if _, err := s.tasks.EnsureStarted(ctx, req.TaskID); err != nil {
		return nil, err
	}

	record := &CleaningRecord{}
	apply(req, record)

	for attempt := 0; attempt < 5; attempt++ {
		record.Code = s.nextCode(ctx, record.CleanedAt)
		err := s.repo.Create(ctx, record)
		if err == nil {
			if err := s.decorate(ctx, record); err != nil {
				return nil, err
			}
			return record, nil
		}
		if !errors.Is(err, gorm.ErrDuplicatedKey) {
			return nil, httpx.WrapInternal("录入清淤记录失败", err)
		}
	}
	return nil, httpx.Conflict("记录编号生成冲突，请稍后重试")
}

// Update 修改清淤记录。任务进入验收流程后不可再修改。
func (s *Service) Update(ctx context.Context, id uint, req SaveRequest) (*CleaningRecord, error) {
	record, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	if req.TaskID != record.TaskID {
		return nil, httpx.InvalidState("清淤记录不支持更换所属任务，如需调整请删除后重新录入")
	}
	task, err := s.tasks.FindByID(ctx, req.TaskID)
	if err != nil {
		return nil, err
	}
	if !editable(task.Status) {
		return nil, httpx.InvalidState(fmt.Sprintf(
			"任务当前状态为「%s」，不能再修改清淤记录", cleaningtask.StatusLabel(task.Status),
		))
	}
	referenced, err := s.repo.HasAcceptance(ctx, id)
	if err != nil {
		return nil, httpx.WrapInternal("检查验收引用失败", err)
	}
	if referenced {
		return nil, httpx.InvalidState("该清淤记录已被验收记录引用，不能再修改")
	}
	if err := validate(req); err != nil {
		return nil, err
	}
	apply(req, record)
	if err := s.repo.Save(ctx, record); err != nil {
		return nil, httpx.WrapInternal("修改清淤记录失败", err)
	}
	if err := s.decorate(ctx, record); err != nil {
		return nil, err
	}
	return record, nil
}

// Delete 删除清淤记录。
func (s *Service) Delete(ctx context.Context, id uint) error {
	record, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return notFound(err)
	}
	task, err := s.tasks.FindByID(ctx, record.TaskID)
	if err != nil {
		return err
	}
	if !editable(task.Status) {
		return httpx.InvalidState(fmt.Sprintf(
			"任务当前状态为「%s」，不能再删除清淤记录", cleaningtask.StatusLabel(task.Status),
		))
	}
	referenced, err := s.repo.HasAcceptance(ctx, id)
	if err != nil {
		return httpx.WrapInternal("检查验收引用失败", err)
	}
	if referenced {
		return httpx.InvalidState("该清淤记录已被验收记录引用，不能再删除")
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return notFound(err)
	}
	return nil
}

// FindByID 查询清淤记录（含折算干重）。
func (s *Service) FindByID(ctx context.Context, id uint) (*CleaningRecord, error) {
	record, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	if err := s.decorate(ctx, record); err != nil {
		return nil, err
	}
	return record, nil
}

// TotalsByTask 汇总任务下的清淤量（供验收模块判断清淤成果）。
func (s *Service) TotalsByTask(ctx context.Context, taskID uint) (refx.RecordTotals, error) {
	totals, err := s.repo.TotalsByTask(ctx, taskID)
	if err != nil {
		return refx.RecordTotals{}, httpx.WrapInternal("统计清淤量失败", err)
	}
	return totals, nil
}

// List 分页查询清淤记录，并补齐所属任务与管段信息。
func (s *Service) List(ctx context.Context, query ListQuery) ([]ListItem, int64, error) {
	records, total, err := s.repo.List(ctx, query)
	if err != nil {
		return nil, 0, httpx.WrapInternal("查询清淤记录失败", err)
	}
	if len(records) == 0 {
		return []ListItem{}, total, nil
	}
	if err := s.decorateAll(ctx, records); err != nil {
		return nil, 0, err
	}

	taskIDs := make([]uint, 0, len(records))
	for i := range records {
		taskIDs = append(taskIDs, records[i].TaskID)
	}
	briefs, err := refx.TaskBriefsByIDs(ctx, s.repo.DB(), taskIDs)
	if err != nil {
		return nil, 0, httpx.WrapInternal("查询任务信息失败", err)
	}

	items := make([]ListItem, 0, len(records))
	for i := range records {
		record := records[i]
		item := ListItem{CleaningRecord: record}
		if brief, ok := briefs[record.TaskID]; ok {
			item.Task = &brief
		}
		items = append(items, item)
	}
	return items, total, nil
}

// Detail 记录详情。
func (s *Service) Detail(ctx context.Context, id uint) (*DetailResponse, error) {
	record, err := s.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	briefs, err := refx.TaskBriefsByIDs(ctx, s.repo.DB(), []uint{record.TaskID})
	if err != nil {
		return nil, httpx.WrapInternal("查询任务信息失败", err)
	}
	detail := &DetailResponse{Record: record}
	if brief, ok := briefs[record.TaskID]; ok {
		detail.Task = &brief
	}
	return detail, nil
}

// editable 判断任务是否处于可以增删改清淤记录的状态。
func editable(taskStatus string) bool {
	return taskStatus == cleaningtask.StatusPending || taskStatus == cleaningtask.StatusInProgress
}

func validate(req SaveRequest) error {
	if req.CleanedAt.IsZero() {
		return httpx.Validation("清淤日期不能为空")
	}
	if req.CleanedAt.After(date.Today()) {
		return httpx.Validation("清淤日期不能晚于今天")
	}
	if !sludge.HasCaliber(strings.TrimSpace(req.SludgeCaliber)) {
		return httpx.Validation(fmt.Sprintf("清淤量计量口径只能是：%s", option.Labels(sludge.CaliberOptions())))
	}
	method := strings.TrimSpace(req.Method)
	if method != "" && !option.Has(cleaningtask.MethodOptions(), method) {
		return httpx.Validation(fmt.Sprintf("清淤方式只能是：%s", option.Labels(cleaningtask.MethodOptions())))
	}
	weather := strings.TrimSpace(req.Weather)
	if weather != "" && !option.Has(WeatherOptions(), weather) {
		return httpx.Validation(fmt.Sprintf("天气只能是：%s", option.Labels(WeatherOptions())))
	}
	if strings.TrimSpace(req.RecorderName) == "" {
		return httpx.Validation("记录人不能为空")
	}
	return nil
}

func apply(req SaveRequest, target *CleaningRecord) {
	target.TaskID = req.TaskID
	target.CleanedAt = req.CleanedAt
	target.LengthM = req.LengthM
	target.SludgeAmount = req.SludgeAmount
	target.SludgeCaliber = strings.TrimSpace(req.SludgeCaliber)
	target.WaterVolumeM3 = req.WaterVolumeM3
	target.PersonnelCount = req.PersonnelCount
	target.Method = strings.TrimSpace(req.Method)
	target.Equipment = strings.TrimSpace(req.Equipment)
	target.Weather = strings.TrimSpace(req.Weather)
	target.SludgeDisposalSite = strings.TrimSpace(req.SludgeDisposalSite)
	target.SafetyMeasures = strings.TrimSpace(req.SafetyMeasures)
	target.ProblemFound = strings.TrimSpace(req.ProblemFound)
	target.RecorderName = strings.TrimSpace(req.RecorderName)
	target.Remark = strings.TrimSpace(req.Remark)
}

// nextCode 生成形如 QJ20260914-0001 的记录编号。
func (s *Service) nextCode(ctx context.Context, cleanedAt date.Date) string {
	prefix := "QJ" + cleanedAt.Format("20060102")
	sequence := 1
	if latest, err := s.repo.MaxCodeWithPrefix(ctx, prefix); err == nil && latest != "" {
		if idx := strings.LastIndex(latest, "-"); idx >= 0 {
			if parsed, err := strconv.Atoi(latest[idx+1:]); err == nil {
				sequence = parsed + 1
			}
		}
	}
	return fmt.Sprintf("%s-%04d", prefix, sequence)
}

func notFound(err error) error {
	if errors.Is(err, ErrNotFound) {
		return httpx.NotFound("清淤记录不存在")
	}
	return httpx.WrapInternal("查询清淤记录失败", err)
}
