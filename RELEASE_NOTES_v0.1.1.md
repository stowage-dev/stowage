## v0.1.1

### Highlights

- **Unified binary.** The operator has been merged into the main `stowage` binary and the Helm chart now ships as a single-replica integrated deployment, simplifying installation and operations.
- **S3 POST Object support.** The proxy now handles browser-style S3 POST uploads, including policy verification, inbound `Location`, CORS, and orphan cleanup.
- **Helm chart distribution via GitHub Pages.** Charts are now published from tagged releases to a GitHub Pages-backed Helm repository.

### Features

- Wire S3 POST Object handler into the proxy with classification and policy verifier (#25)
- Public hostname configuration for the S3 proxy with URL propagation
- Surface `S3Backend` CRs in the admin UI as read-only entries
- Add `metrics.prometheusScrape` toggle for Pod-level Prometheus annotations
- GitHub Pages Helm chart repository workflow (#26), publishing from git tags (#30)
- Improved install script

### UI

- Replace `DataTable` with a TanStack-backed table primitive set
- Replace component CSS with Tailwind utilities, reducing manual styles and bloat
- Align checkbox and icon columns in the object browser
- AI-generated content warning in the README

### Fixes

- Operator metrics listener no longer collides with the main HTTP server
- Operator-minted credentials no longer disappear from the dashboard
- Removed default 200-response logging noise
- Removed QNAP-specific fields from `S3Backend`

### Internal

- Migrate integration tests from envtest to e2e cluster-based testing (#27)
- DCO sign-off `prepare-commit-msg` hook
- Disable API rate limit in demo config

**Full Changelog**: https://github.com/stowage-dev/stowage/compare/v0.1.0...v0.1.1
