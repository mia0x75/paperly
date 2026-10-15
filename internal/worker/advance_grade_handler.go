package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"paperly/internal/domain"
)

// kind=2 (grade advance step 3.a) detail payload.
type advanceGradeDetail struct {
	KidID         int64 `json:"kid_id"`
	EnrolledYear  int   `json:"enrolled_year"`
	EnrolledGrade int   `json:"enrolled_grade"`
	TargetGrade   int   `json:"target_grade"`
}

// Parses and validates detail. A missing required field is an enqueue-side bug, so retrying is
// pointless and the caller treats the error as non-retryable.
func parseAdvanceGradeDetail(raw string) (advanceGradeDetail, error) {
	var d advanceGradeDetail
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return d, fmt.Errorf("parse advance_grade detail: %w", err)
	}
	if d.KidID <= 0 || d.EnrolledYear <= 0 ||
		d.EnrolledGrade < 1 || d.EnrolledGrade > 12 || d.TargetGrade < 1 {
		return d, errors.New("advance_grade detail missing required field or invalid value")
	}
	return d, nil
}

// kind=2 grade advance.
//
// Idempotency gate (single source of truth): advance only when kids.grade == enrolled_grade,
// otherwise skip, respecting a manual parent edit in either direction. raw_target =
// target_grade + (current school year - enrolled_year); ≤12 changes grade, >12 graduates (grade
// stays 12).
//
// Execution is stepwise (change kids → send notice → Worker sets done), with no three-table
// transaction; the idempotency gate ensures a rerun or duplicate enqueue never advances twice.
func handleAdvanceGrade(ctx context.Context, deps Deps, task *domain.Task) error {
	d, err := parseAdvanceGradeDetail(task.Detail)
	if err != nil {
		return nonRetryable(err)
	}

	kid, err := deps.Kids.GetByID(ctx, d.KidID)
	if errors.Is(err, domain.ErrNotFound) {
		return skip("kid not found (possibly soft-deleted)")
	}
	if err != nil {
		return err
	}
	if kid.Status != domain.KidStatusActive {
		return skip("kid is not in active status")
	}
	// Idempotency gate: parent manually changed the grade after enqueue → skip,
	// do not override the parent's decision.
	if kid.Grade != d.EnrolledGrade {
		return skip("grade manually overridden by parent")
	}

	rawTarget := d.TargetGrade + (domain.CurrentSchoolYear(deps.Clock()) - d.EnrolledYear)
	if rawTarget > 12 {
		if err := deps.Kids.Graduate(ctx, d.KidID); err != nil {
			return err
		}
	} else {
		kid.Grade = rawTarget
		if err := deps.Kids.Update(ctx, kid); err != nil {
			return err
		}
	}

	// Notify only after a successful advance; a notice failure does not roll back the result.
	const body = "已自动升一级。如因休学/跳级不符，请到孩子档案修改。"
	link := fmt.Sprintf("/kids/%d", d.KidID)
	if _, nerr := deps.Notify.CreateParentNotice(ctx, domain.MessageKindGradeAdvanced,
		"已自动升级", body, link); nerr != nil {
		deps.Log.Warn("grade-advance notice write failed", "kid_id", d.KidID, "err", nerr)
	}

	// The student hears their own grade changed too (kid audience, bound to kid_id). Graduation
	// (rawTarget>12) leaves the grade at 12 and only flips status, so it stays parent-only.
	if rawTarget <= 12 {
		kidBody := fmt.Sprintf("你的年级已更新为 %d 年级。", rawTarget)
		if _, nerr := deps.Notify.CreateKidNotice(ctx, d.KidID, domain.MessageKindGradeChanged,
			"年级已更新", kidBody, "/kid"); nerr != nil {
			deps.Log.Warn("grade-change kid notice write failed", "kid_id", d.KidID, "err", nerr)
		}
	}

	// Grade 7 is junior high entry, where the subject set broadens (biology, geography, physics,
	// chemistry, history...). Remind the parent to verify the kid's subject whitelist covers them.
	if rawTarget == 7 {
		reminder := fmt.Sprintf("%s升到初一，科目会变多。请到孩子设置中确认科目是否完整。", kid.Name)
		if _, nerr := deps.Notify.CreateParentNotice(ctx, domain.MessageKindGrade7Reminder,
			"升入初中提醒", reminder, fmt.Sprintf("/kids/%d", d.KidID)); nerr != nil {
			deps.Log.Warn("grade-7 reminder write failed", "kid_id", d.KidID, "err", nerr)
		}
	}

	// Grade 9 is when biology/geography wrap up; remind the parent they can hide the finished
	// subjects so they stop appearing on the upload page and radar.
	if rawTarget == 9 {
		reminder := fmt.Sprintf("%s升到初三，生物 / 地理应该已结课。如需隐藏请到科目管理中设置。", kid.Name)
		if _, nerr := deps.Notify.CreateParentNotice(ctx, domain.MessageKindGrade9Reminder,
			"升入初三提醒", reminder, "/settings/subjects"); nerr != nil {
			deps.Log.Warn("grade-9 reminder write failed", "kid_id", d.KidID, "err", nerr)
		}
	}

	// Grade 10 is senior high, where the arts/science split begins; remind the parent they can set
	// the kid's track so subject filtering reflects it.
	if rawTarget == 10 {
		reminder := fmt.Sprintf("%s升到高一。如需设置文理科，请到孩子设置中设置。", kid.Name)
		if _, nerr := deps.Notify.CreateParentNotice(ctx, domain.MessageKindGrade10Reminder,
			"升入高中提醒", reminder, fmt.Sprintf("/kids/%d", d.KidID)); nerr != nil {
			deps.Log.Warn("grade-10 reminder write failed", "kid_id", d.KidID, "err", nerr)
		}
	}
	return nil
}
