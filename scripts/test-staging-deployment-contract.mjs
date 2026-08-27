#!/usr/bin/env node

import assert from "node:assert/strict";
import fs from "node:fs";

const productionWorkflow = fs.readFileSync(".github/workflows/release.yaml", "utf8");
assert.ok(
  productionWorkflow.includes("release-metadata.json"),
  "the production semantic-tag workflow must publish immutable metadata",
);
for (const forbidden of ["GH_PAT", "argocd-apps.git", "git push origin main"]) {
  assert.ok(
    !productionWorkflow.includes(forbidden),
    `the production semantic-tag workflow must not mutate GitOps: ${forbidden}`,
  );
}

const staging = fs.readFileSync(".github/workflows/staging.yaml", "utf8");
for (const required of [
  "github.event.workflow_run.event == 'push'",
  "github.event.workflow_run.head_branch == 'main'",
  "ref: ${{ env.SOURCE_SHA }}",
  "test \"$(git rev-parse FETCH_HEAD)\" = \"$SOURCE_SHA\"",
  "node scripts/update-staging-gitops.mjs",
  "apps/mycoorigyn/api/overlays/staging/",
]) {
  assert.ok(staging.includes(required), `staging workflow is missing required contract: ${required}`);
}
for (const forbidden of [
  "apps/mycoorigyn/api/overlays/production",
  "mycoorigyn-marketing-api-production.yaml",
  "refs/tags/",
  "workflow_dispatch",
]) {
  assert.ok(!staging.includes(forbidden), `staging workflow contains forbidden production/manual contract: ${forbidden}`);
}

console.log("staging deployment contract: PASS");
