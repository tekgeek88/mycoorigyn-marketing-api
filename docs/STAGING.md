# Marketing API staging

The marketing API staging environment is isolated from production and is deployed only from an exact, successfully tested `main` commit.

## Release boundary

- `.github/workflows/release.yaml` remains the production semantic-tag promotion path.
- `.github/workflows/staging.yaml` runs only after the main test workflow passes for a push to `main`.
- The staging workflow checks out the tested SHA, proves it is still current `main`, builds application and migration images tagged `staging-<40-character-source-sha>`, and updates only the staging paths in `tekgeek88/argocd-apps`.
- The staging workflow has no manual-dispatch trigger and refuses any GitOps change outside its four allowlisted staging identity files.

## Isolated state

Staging uses namespace `mycoorigyn-marketing-api-staging`, database `mycoorigyn_marketing_staging`, and role `mycoorigyn_marketing_staging_user`. It has independent Kubernetes Secrets for the database, provisioning bearer credential, and Resend API key. Review and signup capability plaintext uses the staging-only `mycoorigyn-marketing-staging-token-root` PVC.

The internal API endpoint is:

```text
http://mycoorigyn-api.mycoorigyn-marketing-api-staging.svc.cluster.local
```

There is no staging Ingress. Browser/public access requires a separate review and authorization.

## Email safety

Staging requires `MARKETING_EMAIL_ALLOWED_RECIPIENTS`. The server refuses to start without a non-empty valid list and rejects every outgoing message whose normalized recipient is absent. Production retains its existing behavior when the variable is unset.

The initial staging allowlist contains only the operator review address. Add any controlled test applicant explicitly before a later enabled-staging test; never use a real applicant as a canary.

## Activation boundary

Deploying this environment does not authorize an application, approval, grant, hosted signup, tenant, or email. MycoOrigyn hosted signup remains feature-disabled. A later enabled-staging milestone must separately add the required network egress and controlled test authorization.
