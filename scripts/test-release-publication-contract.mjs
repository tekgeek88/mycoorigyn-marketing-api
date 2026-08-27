import { readFileSync } from 'node:fs'

const workflow = readFileSync(new URL('../.github/workflows/release.yaml', import.meta.url), 'utf8')

const forbidden = [
  'GH_PAT',
  'argocd-apps.git',
  'git push origin main',
  'Update ArgoCD GitOps Repo',
  'kustomize edit',
]

for (const value of forbidden) {
  if (workflow.includes(value)) {
    throw new Error(`semantic-tag publication still contains production GitOps mutation token: ${value}`)
  }
}

const required = [
  'contents: read',
  'release-metadata.json',
  'actions/upload-artifact@v4',
  'source_sha=$(git rev-parse HEAD)',
  'docker buildx imagetools inspect',
  'migrationInventorySha256',
  'Migration execution: **none**',
  'Production GitOps mutation: **none**',
]

for (const value of required) {
  if (!workflow.includes(value)) {
    throw new Error(`release publication metadata contract is missing: ${value}`)
  }
}

console.log('marketing release publication contract: PASS')
