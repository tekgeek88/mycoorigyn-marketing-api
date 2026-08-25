package approvals

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/transactionalemail"
)

func buildApprovalMessage(from, replyTo, baseURL string, approval Approval, token string) (transactionalemail.Message, error) {
	link := strings.TrimRight(baseURL, "/") + "#access=" + url.PathEscape(token)
	farm := strings.TrimSpace(approval.FarmName)
	if farm == "" {
		farm = "your farm"
	}
	expires := approval.ExpiresAt.UTC().Format("January 2, 2006 at 15:04 UTC")
	htmlBody, err := transactionalemail.RenderBrandedHTML(transactionalemail.BrandedContent{
		Preheader: "Your MycoOrigyn Early Access request has been approved.",
		Eyebrow:   "Early Access approved",
		Heading:   "You're approved for MycoOrigyn Early Access",
		Intro:     "Your request has been approved. Use your single-use access link to create your farm workspace.",
		Details: []transactionalemail.BrandedDetail{
			{Label: "Farm", Value: farm},
			{Label: "Access link expires", Value: expires},
		},
		Action:         transactionalemail.BrandedAction{Label: "Create your farm", URL: link},
		SectionHeading: "What happens next",
		Steps: []string{
			"Create your farm workspace with the approved email address.",
			"Wait while MycoOrigyn securely prepares your workspace.",
			"Activate the owner account and finish workspace setup.",
		},
		SecurityNote: "This link is single-use. If you were not expecting this approval, do not use the link and contact MycoOrigyn support.",
	})
	if err != nil {
		return transactionalemail.Message{}, err
	}
	textBody := fmt.Sprintf("You're approved for MycoOrigyn Early Access\n\nYour request for %s has been approved.\nAccess link expires: %s\n\nCreate your farm:\n%s\n\nWhat happens next:\n1. Create your farm workspace with the approved email address.\n2. Wait while MycoOrigyn securely prepares your workspace.\n3. Activate the owner account and finish workspace setup.\n\nThis link is single-use. If you were not expecting this approval, do not use it and contact MycoOrigyn support.\n", farm, expires, link)
	return transactionalemail.Message{
		To: approval.ApprovedEmail, From: from, ReplyTo: replyTo,
		Subject: "You're approved for MycoOrigyn Early Access", Text: textBody, HTML: htmlBody,
	}, nil
}
