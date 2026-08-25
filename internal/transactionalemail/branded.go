package transactionalemail

import (
	"bytes"
	"fmt"
	"html/template"
	"net/url"
	"strings"
)

type BrandedDetail struct {
	Label string
	Value string
}

type BrandedAction struct {
	Label string
	URL   string
}

// BrandedContent is the provider-independent presentation contract shared by
// marketing transactional messages. html/template escapes every dynamic field.
type BrandedContent struct {
	Preheader      string
	Eyebrow        string
	Heading        string
	Intro          string
	Details        []BrandedDetail
	Action         BrandedAction
	SectionHeading string
	Steps          []string
	SecurityNote   string
}

var brandedTemplate = template.Must(template.New("mycoorigyn-transactional-email").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="color-scheme" content="dark">
  <title>{{.Heading}}</title>
  <style>
    @media only screen and (max-width: 640px) {
      .email-shell { width: 100% !important; }
      .email-card { padding: 28px 22px !important; }
      .email-heading { font-size: 28px !important; line-height: 34px !important; }
      .email-button { display: block !important; text-align: center !important; }
    }
  </style>
</head>
<body style="margin:0; padding:0; background-color:#06111f; color:#f8fafc; font-family:Inter,-apple-system,BlinkMacSystemFont,'Segoe UI',Arial,sans-serif;">
  <div style="display:none; max-height:0; overflow:hidden; opacity:0; color:transparent; line-height:1px; mso-hide:all;">{{.Preheader}}</div>
  <table role="presentation" width="100%" cellspacing="0" cellpadding="0" border="0" style="width:100%; background-color:#06111f;">
    <tr><td align="center" style="padding:32px 16px;">
      <table role="presentation" width="600" cellspacing="0" cellpadding="0" border="0" class="email-shell" style="width:600px; max-width:600px;">
        <tr><td style="padding:0 4px 18px 4px;">
          <div style="font-size:24px; line-height:30px; font-weight:800; letter-spacing:-0.5px;"><span style="color:#42d8e8;">Myco</span><span style="color:#f6bd60;">Origyn</span></div>
          <div style="padding-top:3px; color:#94a3b8; font-size:13px; line-height:20px;">From culture to harvest, all in one place.</div>
        </td></tr>
        <tr><td style="height:4px; font-size:0; line-height:0; background-color:#42d8e8; background-image:linear-gradient(90deg,#42d8e8 0%,#20c7a7 42%,#9b7cff 72%,#f6bd60 100%); border-radius:8px 8px 0 0;">&nbsp;</td></tr>
        <tr><td class="email-card" style="padding:40px 42px; background-color:#0d2340; border:1px solid #173557; border-top:0; border-radius:0 0 14px 14px; box-shadow:0 18px 45px rgba(0,0,0,0.28);">
          {{if .Eyebrow}}<div style="margin:0 0 10px 0; color:#7ee787; font-size:12px; line-height:18px; font-weight:800; letter-spacing:1.4px; text-transform:uppercase;">{{.Eyebrow}}</div>{{end}}
          <h1 class="email-heading" style="margin:0 0 18px 0; color:#f8fafc; font-size:34px; line-height:41px; font-weight:800; letter-spacing:-0.7px;">{{.Heading}}</h1>
          <p style="margin:0 0 24px 0; color:#cbd5e1; font-size:16px; line-height:26px;">{{.Intro}}</p>
          {{if .Details}}<table role="presentation" width="100%" cellspacing="0" cellpadding="0" border="0" style="width:100%; margin:0 0 26px 0; background-color:#0a1a2f; border:1px solid #1a3c60; border-radius:10px;">
            {{range .Details}}<tr><td style="padding:12px 16px; color:#94a3b8; font-size:13px; line-height:20px; font-weight:700; vertical-align:top; border-bottom:1px solid #173557;">{{.Label}}</td><td style="padding:12px 16px; color:#f8fafc; font-size:14px; line-height:21px; text-align:right; vertical-align:top; border-bottom:1px solid #173557;">{{.Value}}</td></tr>{{end}}
          </table>{{end}}
          <table role="presentation" cellspacing="0" cellpadding="0" border="0" style="margin:0 0 22px 0;"><tr><td bgcolor="#42d8e8" style="border-radius:8px; background-color:#42d8e8; background-image:linear-gradient(90deg,#42d8e8,#20c7a7);"><a class="email-button" href="{{.Action.URL}}" style="display:inline-block; padding:14px 24px; color:#06111f; font-size:16px; line-height:20px; font-weight:800; text-decoration:none; border-radius:8px;">{{.Action.Label}}</a></td></tr></table>
          <p style="margin:0 0 26px 0; color:#94a3b8; font-size:12px; line-height:19px; word-break:break-all;">If the button does not work, copy and paste this secure link into your browser:<br><a href="{{.Action.URL}}" style="color:#42d8e8; text-decoration:underline;">{{.Action.URL}}</a></p>
          {{if .Steps}}<div style="margin:0 0 24px 0; padding:20px; background-color:#0a1a2f; border-left:3px solid #9b7cff; border-radius:8px;"><div style="margin:0 0 10px 0; color:#f8fafc; font-size:15px; line-height:22px; font-weight:800;">{{.SectionHeading}}</div><ol style="margin:0; padding:0 0 0 20px; color:#cbd5e1; font-size:14px; line-height:23px;">{{range .Steps}}<li style="padding:2px 0;">{{.}}</li>{{end}}</ol></div>{{end}}
          {{if .SecurityNote}}<p style="margin:0; padding-top:20px; border-top:1px solid #1a3c60; color:#94a3b8; font-size:13px; line-height:21px;">{{.SecurityNote}}</p>{{end}}
        </td></tr>
        <tr><td align="center" style="padding:22px 18px 8px 18px; color:#64748b; font-size:12px; line-height:19px;"><strong style="color:#94a3b8;">MycoOrigyn</strong><br>From culture to harvest, all in one place.<br>This transactional email was sent because of activity involving MycoOrigyn.</td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`))

func RenderBrandedHTML(content BrandedContent) (string, error) {
	content.Preheader = strings.TrimSpace(content.Preheader)
	content.Eyebrow = strings.TrimSpace(content.Eyebrow)
	content.Heading = strings.TrimSpace(content.Heading)
	content.Intro = strings.TrimSpace(content.Intro)
	content.Action.Label = strings.TrimSpace(content.Action.Label)
	content.Action.URL = strings.TrimSpace(content.Action.URL)
	content.SectionHeading = strings.TrimSpace(content.SectionHeading)
	content.SecurityNote = strings.TrimSpace(content.SecurityNote)
	if content.Preheader == "" || content.Heading == "" || content.Intro == "" || content.Action.Label == "" || !validTransactionalURL(content.Action.URL) {
		return "", fmt.Errorf("transactional email content is incomplete")
	}
	for index := range content.Details {
		content.Details[index].Label = strings.TrimSpace(content.Details[index].Label)
		content.Details[index].Value = strings.TrimSpace(content.Details[index].Value)
		if content.Details[index].Label == "" || content.Details[index].Value == "" {
			return "", fmt.Errorf("transactional email detail is incomplete")
		}
	}
	if len(content.Steps) > 0 && content.SectionHeading == "" {
		return "", fmt.Errorf("transactional email section heading is missing")
	}
	for index := range content.Steps {
		content.Steps[index] = strings.TrimSpace(content.Steps[index])
		if content.Steps[index] == "" {
			return "", fmt.Errorf("transactional email step is empty")
		}
	}
	var output bytes.Buffer
	if err := brandedTemplate.Execute(&output, content); err != nil {
		return "", fmt.Errorf("render transactional email: %w", err)
	}
	return output.String(), nil
}

func validTransactionalURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}
