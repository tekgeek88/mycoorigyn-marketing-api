# 🧪 QME Staging Environment Guide

The staging environment allows developers to test backend features, infrastructure changes, and 
deployment flows in an isolated, production-like setup before releasing to production.

---

## ✅ What Is Staging?

- A Kubernetes namespace (`qme-staging`) managed by ArgoCD
- Syncs from the `staging` branch of the backend repo
- Uses its own Kustomize overlay (`apps/qme/backend/overlays/staging`)
- Deploys backend Docker images tagged as `staging-<sha>`

---

## 🚀 How to Deploy to Staging

1. **Start in a feature branch**
   ```bash
   git checkout -b feature/my-feature
   ```

2. **Do your work and commit as usual**
   
3. **Merge your changes into staging**
   ```bash
   git checkout staging
   git pull
   git merge feature/my-feature
   git push
   ```

📎 Notes
- Do not push directly to production from staging
- Keep staging-specific config changes in the overlays/staging folder
- ArgoCD will auto-sync changes once the manifest is updated

## View deployments (example)
sudo kubectl -n qme-staging get all

## For any staging issues, check ArgoCD or view logs using:
kubectl -n qme-staging logs deploy/staging-qme-backend