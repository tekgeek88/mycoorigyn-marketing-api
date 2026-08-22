# 🚀 Release Process
## When main is stable and ready for production, we cut a new release using the following steps:

# 🧾 Step-by-Step Release Instructions
### 1️⃣ Ensure you're on the latest main branch
### Merge all completed features and bug fixes to main branch (squash and merge)
```
git checkout production
git pull
git checkout main
git pull
```

# 2️⃣ Create a release branch with the target version
```
git checkout -b release/v0.1.0
git merge production
```

# 3️⃣ Update version files and changelog
### This is easiest at the step after you create the PR to prod because it will show you a list of commits

# 4️⃣ Commit your release updates
```
git add .
git commit -m "Release v0.1.0"
```

# 5️⃣ Push release branch to GitHub
```
git push origin release/v0.1.0
```

# 6️⃣ Open a Pull Request:
### - Base: production
### - Compare: release/v0.1.0
### - Get review and approval

# 7️⃣ After merge, create a git tag on production
```
git checkout production
git pull
git tag v0.1.0
git push origin v0.1.0
```

# 8️⃣ Ensure production is up-to-date after the release and merge into main
```
git checkout production
git pull
git checkout main
git pull
git merge production
git push
```




# 🔥 Hotfix Process
## Use this flow when you need to patch a production issue quickly.

# 1️⃣ Checkout and update production
```
git checkout production
git pull
```

# 2️⃣ Create a hotfix branch from production
```
git checkout -b hotfix/QME-1234_hotfix_fix-login-crash
```

# 3️⃣ Make the fix and commit it
```
git add .
git commit -m "Fix login crash affecting production users"
```

# 4️⃣ Push the hotfix branch and open PR → production
```
git push origin hotfix/QME-1234_hotfix_fix-login-crash
```

# Update the changelog

# 5️⃣ Merge PR into production after review
# This deploys the fix to production once tagged

# 6️⃣ Tag the release
```
git checkout production
git pull
git tag v0.1.0
git push origin v0.1.0
```

# 7️⃣ Merge hotfix back into dev to avoid losing the patch
```
git checkout main
git pull
git merge production
git push
```