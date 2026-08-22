package approvals

import (
	"fmt"
	"html"
	"net/url"
	"strings"
	"time"

	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/transactionalemail"
)

func buildApprovalMessage(from, replyTo, baseURL string, approval Approval, token string) transactionalemail.Message {
	link := strings.TrimRight(baseURL, "/") + "#access=" + url.PathEscape(token)
	farm := strings.TrimSpace(approval.FarmName)
	if farm == "" {
		farm = "your farm"
	}
	expires := approval.ExpiresAt.UTC().Format(time.RFC3339)
	textBody := fmt.Sprintf("Your MycoOrigyn early-access request for %s has been approved.\n\nCreate your farm:\n%s\n\nThis single-use link expires %s.\n", farm, link, expires)
	htmlBody := fmt.Sprintf("<h1>You're approved for MycoOrigyn Early Access</h1><p>Your early-access request for %s has been approved.</p><p><a href=\"%s\">Create your farm</a></p><p>This single-use link expires %s.</p>", html.EscapeString(farm), html.EscapeString(link), html.EscapeString(expires))
	return transactionalemail.Message{
		To: approval.ApprovedEmail, From: from, ReplyTo: replyTo,
		Subject: "You're approved for MycoOrigyn Early Access", Text: textBody, HTML: htmlBody,
	}
}
