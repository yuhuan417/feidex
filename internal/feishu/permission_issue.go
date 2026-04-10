package feishu

import (
	"errors"
	"fmt"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

type PermissionIssueDetail struct {
	Key   string
	Value string
}

type PermissionIssueViolation struct {
	Type        string
	Subject     string
	Description string
}

type PermissionIssueFieldViolation struct {
	Field       string
	Value       string
	Description string
}

type PermissionIssueHelp struct {
	URL         string
	Description string
}

type PermissionIssue struct {
	API                  string
	Code                 int
	Message              string
	Cause                string
	LogID                string
	Troubleshooter       string
	Details              []PermissionIssueDetail
	PermissionViolations []PermissionIssueViolation
	FieldViolations      []PermissionIssueFieldViolation
	Helps                []PermissionIssueHelp
}

type permissionIssueProvider interface {
	PermissionIssue() *PermissionIssue
}

type permissionIssueError struct {
	cause error
	issue *PermissionIssue
}

func (e *permissionIssueError) Error() string {
	if e == nil || e.cause == nil {
		return ""
	}
	return e.cause.Error()
}

func (e *permissionIssueError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func (e *permissionIssueError) PermissionIssue() *PermissionIssue {
	if e == nil {
		return nil
	}
	return e.issue
}

func PermissionIssueFromError(err error) (*PermissionIssue, bool) {
	for current := err; current != nil; current = errors.Unwrap(current) {
		if provider, ok := current.(permissionIssueProvider); ok {
			if issue := provider.PermissionIssue(); issue != nil {
				return issue, true
			}
		}
		var codeErr larkcore.CodeError
		if errors.As(current, &codeErr) {
			if issue := permissionIssueFromCodeError("", codeErr.Code, codeErr.Msg, &codeErr, nil, current); issue != nil {
				return issue, true
			}
		}
	}
	return nil, false
}

func wrapPermissionIssue(err error, issue *PermissionIssue) error {
	if err == nil || issue == nil {
		return err
	}
	return &permissionIssueError{cause: err, issue: issue}
}

func permissionIssueFromDirectError(api string, err error) *PermissionIssue {
	if err == nil || !isFeishuAuthOrPermissionFailure(err, "") {
		return nil
	}
	var codeErr larkcore.CodeError
	if errors.As(err, &codeErr) {
		return permissionIssueFromCodeError(api, codeErr.Code, codeErr.Msg, &codeErr, nil, err)
	}
	cause := strings.TrimSpace(err.Error())
	if cause == "" {
		return nil
	}
	return &PermissionIssue{
		API:   strings.TrimSpace(api),
		Cause: cause,
	}
}

func permissionIssueFromCodeError(api string, code int, msg string, codeErr *larkcore.CodeError, apiResp *larkcore.ApiResp, cause error) *PermissionIssue {
	issue := &PermissionIssue{
		API:     strings.TrimSpace(api),
		Code:    code,
		Message: strings.TrimSpace(msg),
	}
	if cause != nil {
		issue.Cause = strings.TrimSpace(cause.Error())
	}
	if codeErr != nil {
		if issue.Code == 0 {
			issue.Code = codeErr.Code
		}
		if issue.Message == "" {
			issue.Message = strings.TrimSpace(codeErr.Msg)
		}
		if codeErr.Err != nil {
			issue.LogID = strings.TrimSpace(codeErr.Err.LogID)
			issue.Troubleshooter = strings.TrimSpace(codeErr.Err.Troubleshooter)
			for _, detail := range codeErr.Err.Details {
				if detail == nil {
					continue
				}
				issue.Details = append(issue.Details, PermissionIssueDetail{
					Key:   strings.TrimSpace(detail.Key),
					Value: strings.TrimSpace(detail.Value),
				})
			}
			for _, violation := range codeErr.Err.PermissionViolations {
				if violation == nil {
					continue
				}
				issue.PermissionViolations = append(issue.PermissionViolations, PermissionIssueViolation{
					Type:        strings.TrimSpace(violation.Type),
					Subject:     strings.TrimSpace(violation.Subject),
					Description: strings.TrimSpace(violation.Description),
				})
			}
			for _, violation := range codeErr.Err.FieldViolations {
				if violation == nil {
					continue
				}
				issue.FieldViolations = append(issue.FieldViolations, PermissionIssueFieldViolation{
					Field:       strings.TrimSpace(violation.Field),
					Value:       strings.TrimSpace(violation.Value),
					Description: strings.TrimSpace(violation.Description),
				})
			}
			for _, help := range codeErr.Err.Helps {
				if help == nil {
					continue
				}
				issue.Helps = append(issue.Helps, PermissionIssueHelp{
					URL:         strings.TrimSpace(help.URL),
					Description: strings.TrimSpace(help.Description),
				})
			}
		}
	}
	if issue.LogID == "" && apiResp != nil {
		issue.LogID = strings.TrimSpace(apiResp.LogId())
	}
	if !permissionIssueLikelyRelevant(issue) {
		return nil
	}
	return issue
}

func permissionIssueLikelyRelevant(issue *PermissionIssue) bool {
	if issue == nil {
		return false
	}
	if len(issue.PermissionViolations) > 0 || len(issue.Helps) > 0 || strings.TrimSpace(issue.Troubleshooter) != "" {
		return true
	}
	return isFeishuAuthOrPermissionFailure(nil, strings.TrimSpace(fmt.Sprintf("%s %s %s", issue.Message, issue.Cause, flattenPermissionIssueDetails(issue))))
}

func flattenPermissionIssueDetails(issue *PermissionIssue) string {
	if issue == nil {
		return ""
	}
	parts := make([]string, 0, len(issue.Details)+len(issue.FieldViolations))
	for _, detail := range issue.Details {
		parts = append(parts, strings.TrimSpace(detail.Key)+" "+strings.TrimSpace(detail.Value))
	}
	for _, violation := range issue.FieldViolations {
		parts = append(parts, strings.TrimSpace(violation.Field)+" "+strings.TrimSpace(violation.Value)+" "+strings.TrimSpace(violation.Description))
	}
	return strings.Join(parts, " ")
}
