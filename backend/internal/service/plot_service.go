package service

import (
	"errors"
	"fmt"
	"log/slog"

	"gorm.io/gorm"

	"github.com/communitygarden/server/internal/constants"
	"github.com/communitygarden/server/internal/dto"
	"github.com/communitygarden/server/internal/model"
	"github.com/communitygarden/server/internal/repository"
	"github.com/communitygarden/server/internal/util"
)

// InviteGranter 地块释放后邀请最早候补者（由 WaitlistService 实现，避免构造函数循环依赖）。
type InviteGranter interface {
	InviteEarliestWaitingWithTx(tx *gorm.DB, plotID uint) error
}

// InviteGuard 认养时校验/兑现优先认养资格（由 WaitlistService 实现）。
type InviteGuard interface {
	// GetInvitedEntry 返回地块当前受邀申请，无受邀资格时返回 nil。
	GetInvitedEntry(tx *gorm.DB, plotID uint) (*model.WaitlistEntry, error)
	// FulfillInviteWithTx 将受邀人的申请置为 adopted。
	FulfillInviteWithTx(tx *gorm.DB, plotID, userID uint) error
}

// PlotService 地块服务（认养使用事务 + SELECT FOR UPDATE）。
type PlotService struct {
	plotRepo repository.PlotRepository
	db       *gorm.DB
	logger   *slog.Logger

	// 候补认养钩子（main.go 装配时注入；未注入时走原有认养/释放流程）。
	inviteGranter InviteGranter
	inviteGuard   InviteGuard
}

// NewPlotService 构造地块服务。
func NewPlotService(plotRepo repository.PlotRepository, db *gorm.DB, logger *slog.Logger) *PlotService {
	return &PlotService{plotRepo: plotRepo, db: db, logger: logger}
}

// SetWaitlistHooks 注入候补认养服务钩子（避免构造函数循环依赖）。
func (s *PlotService) SetWaitlistHooks(granter InviteGranter, guard InviteGuard) {
	s.inviteGranter = granter
	s.inviteGuard = guard
}

// GetByID 查询地块详情（被地块 handler 与种植计划 service 复用）。
func (s *PlotService) GetByID(id uint) (*model.Plot, error) {
	p, err := s.plotRepo.FindByID(id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, util.NewAppError(constants.CodeNotFound, 404, fmt.Sprintf("地块实体 id=%d 不存在", id))
		}
		return nil, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	return p, nil
}

// Create 创建地块（管理员）。
func (s *PlotService) Create(req *dto.CreatePlotRequest, operator string) (*model.Plot, error) {
	if _, err := s.plotRepo.FindByCode(req.Code); err == nil {
		return nil, util.NewAppError(constants.CodeConflict, 409, fmt.Sprintf("地块编号 %s 已存在", req.Code))
	}
	p := &model.Plot{
		Name:        req.Name,
		Code:        req.Code,
		Area:        req.Area,
		SoilType:    req.SoilType,
		Sunlight:    req.Sunlight,
		Latitude:    req.Latitude,
		Longitude:   req.Longitude,
		Status:      string(constants.PlotStatusAvailable),
		Description: req.Description,
	}
	if err := s.plotRepo.Create(p); err != nil {
		return nil, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	s.logger.Info(constants.LogPlotCreated, "plot_id", p.ID, "code", p.Code, "operator", operator)
	return p, nil
}

// Update 更新地块（管理员）。
func (s *PlotService) Update(id uint, req *dto.UpdatePlotRequest, operator string) (*model.Plot, error) {
	p, err := s.plotRepo.FindByID(id)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, util.NewAppError(constants.CodeNotFound, 404, fmt.Sprintf("地块实体 id=%d 不存在", id))
		}
		return nil, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	if req.Name != nil {
		p.Name = *req.Name
	}
	if req.Code != nil {
		p.Code = *req.Code
	}
	if req.Area != nil {
		p.Area = *req.Area
	}
	if req.SoilType != nil {
		p.SoilType = *req.SoilType
	}
	if req.Sunlight != nil {
		p.Sunlight = *req.Sunlight
	}
	if req.Latitude != nil {
		p.Latitude = *req.Latitude
	}
	if req.Longitude != nil {
		p.Longitude = *req.Longitude
	}
	if req.Description != nil {
		p.Description = *req.Description
	}
	if err := s.plotRepo.Update(p); err != nil {
		return nil, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	return p, nil
}

// List 分页查询地块（可过滤状态）。
func (s *PlotService) List(pq util.PageQuery, status string) ([]model.Plot, int64, error) {
	plots, total, err := s.plotRepo.List(pq, status)
	if err != nil {
		return nil, 0, util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
	}
	return plots, total, nil
}

// Adopt 认养地块（事务 + 行锁，available -> adopted）。
func (s *PlotService) Adopt(plotID, userID uint, role, username string) (*model.Plot, error) {
	var adopted *model.Plot
	err := s.db.Transaction(func(tx *gorm.DB) error {
		plot, err := s.plotRepo.FindByIDForUpdate(tx, plotID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return util.NewAppError(constants.CodeNotFound, 404, fmt.Sprintf("地块实体 id=%d 不存在", plotID))
			}
			return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
		}
		if plot.Status != string(constants.PlotStatusAvailable) {
			return util.NewAppError(constants.CodePlotNotAvailable, 409, fmt.Sprintf("地块 %s 当前状态为 %s，不可认养", plot.Code, util.PlotStatusText(plot.Status)))
		}
		// 候补优先认养：地块释放后已邀请最早候补者时，仅受邀本人可认养，
		// 该资格在受邀人认养或取消前一直有效。
		if s.inviteGuard != nil {
			invited, err := s.inviteGuard.GetInvitedEntry(tx, plotID)
			if err != nil {
				return err
			}
			if invited != nil && invited.UserID != userID {
				return util.NewAppError(constants.CodeWaitlistInviteMismatch, 409,
					fmt.Sprintf("地块 %s 的优先认养资格属于候补用户 %d，请等待其认养或取消", plot.Code, invited.UserID))
			}
		}
		plot.Status = string(constants.PlotStatusAdopted)
		plot.AdopterID = &userID
		if err := s.plotRepo.UpdateWithTx(tx, plot); err != nil {
			return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
		}
		if s.inviteGuard != nil {
			if err := s.inviteGuard.FulfillInviteWithTx(tx, plotID, userID); err != nil {
				return err
			}
		}
		adopted = plot
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.logger.Info(constants.LogPlotAdopted, "plot_id", adopted.ID, "code", adopted.Code, "user_id", userID, "role", role)
	return adopted, nil
}

// Release 释放地块（管理员或认养人，harvested -> available）。
func (s *PlotService) Release(plotID, operatorID uint, operatorRole string) (*model.Plot, error) {
	var released *model.Plot
	err := s.db.Transaction(func(tx *gorm.DB) error {
		plot, err := s.plotRepo.FindByIDForUpdate(tx, plotID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return util.NewAppError(constants.CodeNotFound, 404, fmt.Sprintf("地块实体 id=%d 不存在", plotID))
			}
			return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
		}
		if operatorRole != string(constants.RoleAdmin) && (plot.AdopterID == nil || *plot.AdopterID != operatorID) {
			return util.NewAppError(constants.CodeForbidden, 403, fmt.Sprintf("角色 %s 无权释放地块 %s", util.RoleText(operatorRole), plot.Code))
		}
		if plot.Status != string(constants.PlotStatusHarvested) {
			return util.NewAppError(constants.CodePlotNotAvailable, 409, fmt.Sprintf("地块 %s 当前状态为 %s，仅待释放状态可释放", plot.Code, util.PlotStatusText(plot.Status)))
		}
		plot.Status = string(constants.PlotStatusAvailable)
		plot.AdopterID = nil
		if err := s.plotRepo.UpdateWithTx(tx, plot); err != nil {
			return util.NewAppError(constants.CodeInternalError, 500, constants.ErrorText[constants.CodeInternalError]).Wrap(err)
		}
		// 地块释放：最早候补申请者获得优先认养资格（在其认养或取消前一直有效）。
		if s.inviteGranter != nil {
			if err := s.inviteGranter.InviteEarliestWaitingWithTx(tx, plotID); err != nil {
				return err
			}
		}
		released = plot
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.logger.Info(constants.LogPlotReleased, "plot_id", released.ID, "code", released.Code, "operator", operatorID)
	return released, nil
}

// MarkHarvested 种植计划完成后将地块置为待释放（harvested）。
func (s *PlotService) MarkHarvested(tx *gorm.DB, plotID uint) error {
	plot, err := s.plotRepo.FindByIDForUpdate(tx, plotID)
	if err != nil {
		return err
	}
	plot.Status = string(constants.PlotStatusHarvested)
	return s.plotRepo.UpdateWithTx(tx, plot)
}

// CountByStatus 地块状态统计（仪表盘复用）。
func (s *PlotService) CountByStatus() (map[string]int64, error) {
	return s.plotRepo.CountByStatus()
}
