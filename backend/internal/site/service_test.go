package site

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/TokenFlux/TokenRouter/internal/pkg/pagination"
)

type announcementRepoStub struct {
	item              *Announcement
	items             []Announcement
	archiveExpiredErr error
	callOrder         []string
}

func (s *announcementRepoStub) Create(_ context.Context, a *Announcement) error {
	s.item = a
	return nil
}

func (s *announcementRepoStub) GetByID(_ context.Context, _ int64) (*Announcement, error) {
	if s.item == nil {
		return nil, ErrAnnouncementNotFound
	}
	return s.item, nil
}

func (s *announcementRepoStub) Update(_ context.Context, a *Announcement) error {
	s.item = a
	return nil
}

func (*announcementRepoStub) Delete(context.Context, int64) error {
	return nil
}

func (s *announcementRepoStub) ArchiveExpired(context.Context, time.Time) (int64, error) {
	s.callOrder = append(s.callOrder, "archive")
	return 0, s.archiveExpiredErr
}

func (s *announcementRepoStub) List(context.Context, pagination.PaginationParams, AnnouncementListFilters) ([]Announcement, *pagination.PaginationResult, error) {
	s.callOrder = append(s.callOrder, "list")
	return s.items, &pagination.PaginationResult{}, nil
}

func (*announcementRepoStub) ListActive(context.Context, time.Time) ([]Announcement, error) {
	return nil, nil
}

func TestAnnouncementServiceCreateRejectsEqualStartEndTimes(t *testing.T) {
	repo := &announcementRepoStub{}
	svc := NewAnnouncementService(repo, nil, nil, nil)
	now := time.Unix(1776790020, 0)

	_, err := svc.Create(context.Background(), &CreateAnnouncementInput{
		Title:      "公告",
		Content:    "内容",
		Status:     AnnouncementStatusActive,
		NotifyMode: AnnouncementNotifyModePopup,
		StartsAt:   &now,
		EndsAt:     &now,
	})
	require.ErrorIs(t, err, ErrAnnouncementInvalidSchedule)
}

func TestAnnouncementServiceUpdateRejectsEqualStartEndTimes(t *testing.T) {
	repo := &announcementRepoStub{
		item: &Announcement{
			ID:         1,
			Title:      "公告",
			Content:    "内容",
			Status:     AnnouncementStatusActive,
			NotifyMode: AnnouncementNotifyModePopup,
		},
	}
	svc := NewAnnouncementService(repo, nil, nil, nil)
	now := time.Unix(1776790020, 0)
	startsAt := &now
	endsAt := &now

	_, err := svc.Update(context.Background(), 1, &UpdateAnnouncementInput{
		StartsAt: &startsAt,
		EndsAt:   &endsAt,
	})
	require.ErrorIs(t, err, ErrAnnouncementInvalidSchedule)
}

// TestAnnouncementServiceListArchivesExpiredBeforeQuery 检查管理端查询前归档到期公告的调用顺序。
func TestAnnouncementServiceListArchivesExpiredBeforeQuery(t *testing.T) {
	repo := &announcementRepoStub{items: []Announcement{{ID: 1, Status: AnnouncementStatusArchived}}}
	svc := NewAnnouncementService(repo, nil, nil, nil)

	items, _, err := svc.List(context.Background(), pagination.PaginationParams{}, AnnouncementListFilters{})

	require.NoError(t, err)
	require.Equal(t, []string{"archive", "list"}, repo.callOrder)
	require.Equal(t, AnnouncementStatusArchived, items[0].Status)
}

// TestAnnouncementServiceListStopsWhenArchivingExpiredFails 检查归档失败时列表查询返回该错误。
func TestAnnouncementServiceListStopsWhenArchivingExpiredFails(t *testing.T) {
	archiveErr := errors.New("archive failed")
	repo := &announcementRepoStub{archiveExpiredErr: archiveErr}
	svc := NewAnnouncementService(repo, nil, nil, nil)

	_, _, err := svc.List(context.Background(), pagination.PaginationParams{}, AnnouncementListFilters{})

	require.ErrorIs(t, err, archiveErr)
	require.Equal(t, []string{"archive"}, repo.callOrder)
}

// TestAnnouncementServiceCreateArchivesAlreadyExpiredAnnouncement 检查新建已过期公告时将展示中状态转为已归档。
func TestAnnouncementServiceCreateArchivesAlreadyExpiredAnnouncement(t *testing.T) {
	repo := &announcementRepoStub{}
	svc := NewAnnouncementService(repo, nil, nil, nil)
	endedAt := time.Now().Add(-time.Hour)

	created, err := svc.Create(context.Background(), &CreateAnnouncementInput{
		Title:      "公告",
		Content:    "内容",
		Status:     AnnouncementStatusActive,
		NotifyMode: AnnouncementNotifyModeSilent,
		EndsAt:     &endedAt,
	})

	require.NoError(t, err)
	require.Equal(t, AnnouncementStatusArchived, created.Status)
}

// TestAnnouncementServiceUpdateArchivesAlreadyExpiredAnnouncement 检查更新结束时间后对已过期公告的自动归档。
func TestAnnouncementServiceUpdateArchivesAlreadyExpiredAnnouncement(t *testing.T) {
	repo := &announcementRepoStub{item: &Announcement{
		ID:         1,
		Title:      "公告",
		Content:    "内容",
		Status:     AnnouncementStatusActive,
		NotifyMode: AnnouncementNotifyModeSilent,
	}}
	svc := NewAnnouncementService(repo, nil, nil, nil)
	endedAt := time.Now().Add(-time.Hour)
	endsAt := &endedAt

	updated, err := svc.Update(context.Background(), 1, &UpdateAnnouncementInput{EndsAt: &endsAt})

	require.NoError(t, err)
	require.Equal(t, AnnouncementStatusArchived, updated.Status)
}
