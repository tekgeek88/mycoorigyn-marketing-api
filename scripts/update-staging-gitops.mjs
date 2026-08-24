#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";

const [gitopsRoot, sourceSha, imageTag] = process.argv.slice(2);

if (!gitopsRoot || !/^[0-9a-f]{40}$/.test(sourceSha ?? "")) {
  throw new Error("usage: update-staging-gitops.mjs <gitops-root> <40-char-source-sha> <image-tag>");
}
if (imageTag !== `staging-${sourceSha}`) {
  throw new Error("staging image tag must be derived from the exact source SHA");
}

function replaceExactlyOnce(relativePath, pattern, replacement) {
  const filePath = path.join(gitopsRoot, relativePath);
  const original = fs.readFileSync(filePath, "utf8");
  const matches = original.match(pattern);
  if (!matches || matches.length !== 1) {
    throw new Error(`${relativePath}: expected exactly one staging identity match`);
  }
  const updated = original.replace(pattern, replacement);
  fs.writeFileSync(filePath, updated);
}

replaceExactlyOnce(
  "apps/applications/mycoorigyn-marketing-api-staging.yaml",
  /mycoorigyn\.com\/source-revision: "[0-9a-f]{40}"/g,
  `mycoorigyn.com/source-revision: "${sourceSha}"`,
);
replaceExactlyOnce(
  "apps/mycoorigyn/api/overlays/staging/deployment.yaml",
  /mycoorigyn\.com\/source-revision: "[0-9a-f]{40}"/g,
  `mycoorigyn.com/source-revision: "${sourceSha}"`,
);
replaceExactlyOnce(
  "apps/mycoorigyn/api/overlays/staging/migrations/db-migrate-job.yaml",
  /mycoorigyn\.com\/source-revision: "[0-9a-f]{40}"/g,
  `mycoorigyn.com/source-revision: "${sourceSha}"`,
);
replaceExactlyOnce(
  "apps/mycoorigyn/api/overlays/staging/migrations/db-migrate-job.yaml",
  /name: mycoorigyn-marketing-staging-migrate-[0-9a-f]{12}/g,
  `name: mycoorigyn-marketing-staging-migrate-${sourceSha.slice(0, 12)}`,
);
replaceExactlyOnce(
  "apps/mycoorigyn/api/overlays/staging/migrations/db-migrate-job.yaml",
  /image: tekgeek88\/mycoorigyn-marketing-migrations:\S+/g,
  `image: tekgeek88/mycoorigyn-marketing-migrations:${imageTag}`,
);

const kustomization = "apps/mycoorigyn/api/overlays/staging/kustomization.yaml";
replaceExactlyOnce(
  kustomization,
  /(name: tekgeek88\/mycoorigyn-marketing-api\n\s+newName: tekgeek88\/mycoorigyn-marketing-api\n\s+newTag:) \S+/g,
  `$1 ${imageTag}`,
);
replaceExactlyOnce(
  kustomization,
  /(name: tekgeek88\/mycoorigyn-marketing-migrations\n\s+newName: tekgeek88\/mycoorigyn-marketing-migrations\n\s+newTag:) \S+/g,
  `$1 ${imageTag}`,
);
