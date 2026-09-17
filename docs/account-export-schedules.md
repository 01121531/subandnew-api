# Scheduled Account Exports

Create a schedule from account details or account output. Manage schedules and
individual delivery results in Export Records > Scheduled Tasks. Calendar times
use Asia/Shanghai. A new schedule is paused until explicitly enabled.

## Scope and Reports

- Dynamic filters use all matching accounts in the latest successful local
  snapshots, independent of the visible page. Fixed scopes store account IDs.
- Account selection does not collect upstream accounts. Supported period usage
  uses the existing manual export queries. Unsupported period metrics remain
  unavailable, never substituted with a different period's totals.
- Each run freezes the selection and report window. Its workbook includes
  snapshot timestamps, stale markers, and missing fixed-account warnings.
- The limit is 10,000 accounts per run. Empty results do not send email.
- Existing export records retain files for 30 days.

## Delivery and Recovery

- Enable SMTP before enabling or executing a schedule. Paused schedules can be
  saved without SMTP. Each recipient receives a separate XLSX attachment.
- The unencoded attachment limit is 15 MB. Larger files remain downloadable.
- Explicit temporary SMTP rejections retry at 1, 5, and 15 minutes, using the
  original file. Interrupted or uncertain deliveries require manual verification
  before retry. An acknowledged SMTP acceptance is not retried after a lost QUIT.
- Only Master schedules work. Database claims and occurrence uniqueness prevent
  duplicate runs. Runs for the same plan do not overlap. Missed downtime cycles
  are skipped; Run Now does not change the regular next execution time.
- Pause or delete prevents future runs and cancels unsent deliveries. Existing
  files remain. Already accepted email cannot be recalled.

## Permissions and Upgrade

Ordinary administrators manage their own schedules; superadministrators can
manage all schedules. Every execution, generation, delivery, and download checks
the owner's current instance and field permissions. Revocation blocks access to
old files containing newly hidden fields and suspends affected plans.

The upgrade adds schedule, run, and recipient-delivery tables, plus the schedule
association on existing export records. Back up the database, master key, and
private file directories before upgrading. No existing manual export format or
SMTP credentials are replaced. This release does not deploy to any server.
