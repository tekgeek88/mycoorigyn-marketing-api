# Release process

MycoOrigyn marketing uses semantic versions (`vMAJOR.MINOR.PATCH`) for immutable
artifact publication. Publication, migration execution, and production
promotion are separate operations.

## Publish artifacts

1. Merge the reviewed source to the intended release branch.
2. Create an exact semantic tag on that reviewed commit.
3. Push the tag.
4. `.github/workflows/release.yaml` builds and pushes the versioned API and
   migrations images.
5. The workflow verifies paired migration files, resolves both registry
   digests, and uploads `release-metadata.json` containing version, source SHA,
   image repositories/digests, migration-pair count, and migration inventory
   hash.

The tag workflow has read-only repository contents permission. It does not
clone `tekgeek88/argocd-apps`, use a GitOps PAT, update production manifests,
execute migrations, or push to a GitOps branch. Publishing a tag never deploys
production.

## Qualify and promote

Pin the immutable metadata in staging and complete the normal service,
migration, email, and closed-alpha qualification. A production proposal belongs
in one reviewed `argocd-apps` PR and machine-readable promotion plan. Merging
that PR remains an explicit owner decision.

The production orchestrator runs the exact migration image as a gated Argo
PreSync job, verifies the expected migration delta and clean schema, then admits
later Runtime and landing phases only after marketing is healthy.

```text
source merged
  -> semantic tag publishes API/migrations metadata
  -> staging qualified
  -> production promotion PR/plan reviewed
  -> owner authorizes merge
  -> PreSync migration gate and ordered promotion execute
```

Hotfixes use the same separation. Never edit or push production GitOps from
this repository's release workflow.
