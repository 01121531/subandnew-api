# Third-Party License Inventory

This distribution includes third-party Go and JavaScript dependencies. The
authoritative dependency versions are recorded in `go.mod`, `go.sum`,
`web/package.json`, `web/bun.lock`, and `web/default/package.json`.

## Go dependencies

| License | Direct dependencies |
| --- | --- |
| Apache-2.0 | `github.com/bytedance/gopkg`, `github.com/casbin/casbin/v2`, `github.com/grafana/pyroscope-go`, `github.com/pquerna/otp` |
| BSD-2-Clause / BSD-3-Clause | `github.com/go-redis/redis/v8`, `github.com/go-webauthn/webauthn`, `github.com/google/uuid`, `github.com/pkg/errors`, `github.com/shirou/gopsutil`, `golang.org/x/crypto`, `golang.org/x/net`, `golang.org/x/sync`, `golang.org/x/sys`, `golang.org/x/text` |
| MIT | `github.com/Azure/go-ntlmssp`, `github.com/gin-contrib/gzip`, `github.com/gin-contrib/sessions`, `github.com/gin-contrib/static`, `github.com/gin-gonic/gin`, `github.com/glebarez/sqlite`, `github.com/go-playground/validator/v10`, `github.com/joho/godotenv`, `github.com/nicksnyder/go-i18n/v2`, `github.com/samber/lo`, `github.com/stretchr/testify`, `github.com/tidwall/gjson`, `gopkg.in/yaml.v3`, `gorm.io/driver/mysql`, `gorm.io/driver/postgres`, `gorm.io/gorm` |

## Web dependencies

The web bundle contains dependencies under permissive licenses including MIT,
Apache-2.0, BSD, and ISC. Direct packages include React, TanStack Query/Router,
TanStack Table, Base UI, Tailwind CSS, Rsbuild, i18next, Lucide, Motion,
Zustand, Zod, Axios, DOMPurify, and their transitive dependency graph recorded
in `web/bun.lock`.

Minified frontend chunks retain linked license comments and generated
`*.LICENSE.txt` files. Individual license texts and copyright notices are
available in each package directory in the installed module cache.

## Upstream project

This project is derived from HUICHUAN-AI and remains licensed under AGPL-3.0.
See `LICENSE` and `NOTICE` for the project license and required attribution.
