/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import {
  CheckCircle2,
  Download,
  ExternalLink,
  Loader2,
  RefreshCw,
  RotateCcw,
  ShieldCheck,
  TriangleAlert,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { IconGithub } from '@/assets/brand-icons'
import { Dialog } from '@/components/dialog'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Markdown } from '@/components/ui/markdown'
import { Progress } from '@/components/ui/progress'
import { Skeleton } from '@/components/ui/skeleton'
import { formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  getLatestSystemUpdate,
  getSystemUpdateCapability,
  getSystemUpdateStatus,
  startSystemUpdate,
} from '../api'
import type {
  SystemUpdateCapability,
  SystemUpdatePhase,
  SystemUpdateRelease,
  SystemUpdateState,
} from '../types'

const UPDATE_REPOSITORY_URL = 'https://github.com/01121531/subandnew-api'
const STATUS_POLL_INTERVAL_MS = 1500
const ACTIVE_PHASES = new Set<SystemUpdatePhase>([
  'downloading',
  'verifying',
  'staged',
  'restarting',
  'validating',
  'rolling_back',
])
const TERMINAL_PHASES = new Set<SystemUpdatePhase>([
  'succeeded',
  'failed',
  'rolled_back',
])

function getErrorMessage(error: unknown, fallback: string) {
  if (
    typeof error === 'object' &&
    error !== null &&
    'response' in error &&
    typeof error.response === 'object' &&
    error.response !== null &&
    'data' in error.response
  ) {
    const data = error.response.data
    if (
      typeof data === 'object' &&
      data !== null &&
      'message' in data &&
      typeof data.message === 'string'
    ) {
      return data.message
    }
  }
  return error instanceof Error ? error.message : fallback
}

function UpdatePhaseIcon({
  phase,
  large = false,
}: {
  phase?: SystemUpdatePhase
  large?: boolean
}) {
  const className = large ? 'size-8' : 'size-4'
  if (phase === 'succeeded') {
    return <CheckCircle2 className={cn(className, 'text-emerald-500')} />
  }
  if (phase === 'failed' || phase === 'rolled_back') {
    return <TriangleAlert className={cn(className, 'text-amber-500')} />
  }
  return <Loader2 className={cn(className, 'text-primary animate-spin')} />
}

export function SystemUpdatePanel() {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(true)
  const [checking, setChecking] = useState(false)
  const [installing, setInstalling] = useState(false)
  const [releaseOpen, setReleaseOpen] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [progressOpen, setProgressOpen] = useState(false)
  const [release, setRelease] = useState<SystemUpdateRelease | null>(null)
  const [capability, setCapability] = useState<SystemUpdateCapability | null>(
    null
  )
  const [updateState, setUpdateState] = useState<SystemUpdateState | null>(null)
  const [reconnecting, setReconnecting] = useState(false)
  const terminalToastRef = useRef<SystemUpdatePhase | null>(null)

  const active = updateState ? ACTIVE_PHASES.has(updateState.phase) : false

  const reasonLabel = useCallback(
    (reason?: string) => {
      const labels: Record<string, string> = {
        already_latest: t('The current version is already the latest.'),
        container_managed_externally: t(
          'Container deployments must be upgraded by Docker or Kubernetes.'
        ),
        development_build: t('Development builds cannot be upgraded online.'),
        disabled_by_environment: t(
          'Online updates are disabled by the server environment.'
        ),
        executable_not_replaceable: t(
          'The current executable cannot be replaced.'
        ),
        executable_path_unavailable: t(
          'The current executable path is unavailable.'
        ),
        instance_detection_failed: t(
          'The active server instance topology could not be verified.'
        ),
        matching_asset_missing: t(
          'This release does not contain a matching update package for the current platform.'
        ),
        multi_instance_deployment: t(
          'Multi-instance deployments must be upgraded by an external rolling update system.'
        ),
        not_master_node: t('Online updates can only run on the master node.'),
        release_asset_size_invalid: t('The release asset size is invalid.'),
        release_not_stable: t(
          'Prerelease versions cannot be installed online.'
        ),
        release_tag_not_semver: t(
          'This release uses an invalid version tag. Publish a vMAJOR.MINOR.PATCH release to enable online installation.'
        ),
        unsupported_platform: t(
          'Online updates currently support Windows, Linux, and macOS standalone deployments only.'
        ),
        version_overridden_by_environment: t(
          'Remove the VERSION environment override before using online updates.'
        ),
      }
      return reason ? labels[reason] || reason : ''
    },
    [t]
  )

  const phaseLabel = useCallback(
    (phase?: SystemUpdatePhase) => {
      const labels: Record<SystemUpdatePhase, string> = {
        idle: t('Ready to check for updates'),
        downloading: t('Downloading update package...'),
        verifying: t('Verifying SHA-256 checksum...'),
        staged: t('Update verified and ready to restart...'),
        restarting: t('Restarting the service...'),
        validating: t('Waiting for the new version to become healthy...'),
        succeeded: t('Online update completed successfully.'),
        failed: t('Online update failed.'),
        rolling_back: t('New version is unhealthy. Rolling back...'),
        rolled_back: t('The previous version has been restored.'),
      }
      return labels[phase || 'idle']
    },
    [t]
  )

  const refreshStatus = useCallback(async () => {
    try {
      const state = await getSystemUpdateStatus()
      setUpdateState(state)
      setReconnecting(false)
      if (ACTIVE_PHASES.has(state.phase)) setProgressOpen(true)
      if (
        TERMINAL_PHASES.has(state.phase) &&
        terminalToastRef.current !== state.phase
      ) {
        terminalToastRef.current = state.phase
        if (state.phase === 'succeeded') {
          toast.success(t('Online update completed successfully.'))
        } else if (state.phase === 'rolled_back') {
          toast.warning(t('The previous version has been restored.'))
        } else {
          toast.error(t('Online update failed.'))
        }
      }
      return state
    } catch {
      if (active) setReconnecting(true)
      return null
    }
  }, [active, t])

  useEffect(() => {
    let cancelled = false
    void Promise.all([getSystemUpdateCapability(), getSystemUpdateStatus()])
      .then(([nextCapability, state]) => {
        if (cancelled) return
        setCapability(nextCapability)
        setUpdateState(state)
        if (ACTIVE_PHASES.has(state.phase)) setProgressOpen(true)
      })
      .catch(() => {
        if (!cancelled) setCapability(null)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  useEffect(() => {
    if (!active && !reconnecting) return
    const timer = window.setInterval(() => {
      void refreshStatus()
    }, STATUS_POLL_INTERVAL_MS)
    return () => window.clearInterval(timer)
  }, [active, reconnecting, refreshStatus])

  const capabilityText = useMemo(() => {
    if (!capability) return t('Unable to determine online update capability.')
    if (capability.supported) {
      return t('Online update is available for {{platform}}/{{arch}}.', {
        platform: capability.platform,
        arch: capability.arch,
      })
    }
    return reasonLabel(capability.reason)
  }, [capability, reasonLabel, t])

  const currentVersion =
    release?.current_version || updateState?.current_version || t('Unknown')

  const handleCheck = async () => {
    setChecking(true)
    try {
      const data = await getLatestSystemUpdate()
      setRelease(data)
      if (!data.update_available) {
        toast.success(
          t('You are running the latest version ({{version}}).', {
            version: data.current_version,
          })
        )
        return
      }
      setReleaseOpen(true)
    } catch (error) {
      toast.error(getErrorMessage(error, t('Failed to check for updates')))
    } finally {
      setChecking(false)
    }
  }

  const handleInstall = async () => {
    if (!release) return
    setInstalling(true)
    try {
      const state = await startSystemUpdate(release.id)
      setUpdateState(state)
      terminalToastRef.current = null
      setConfirmOpen(false)
      setReleaseOpen(false)
      setProgressOpen(true)
      toast.success(t('The update task has started.'))
    } catch (error) {
      toast.error(getErrorMessage(error, t('Failed to start online update')))
    } finally {
      setInstalling(false)
    }
  }

  return (
    <>
      <section className='bg-card overflow-hidden rounded-lg border shadow-xs'>
        <div className='flex flex-col gap-3 border-b px-4 py-3 sm:flex-row sm:items-center sm:justify-between sm:px-5'>
          <div className='flex min-w-0 items-center gap-2'>
            <span className='bg-muted text-muted-foreground inline-flex size-7 shrink-0 items-center justify-center rounded-md'>
              <IconGithub className='size-4' aria-hidden='true' />
            </span>
            <div className='min-w-0'>
              <h3 className='text-sm font-semibold'>
                {t('GitHub online update')}
              </h3>
              <p className='text-muted-foreground mt-0.5 text-xs'>
                {t(
                  'Check stable GitHub Releases and securely replace the standalone server binary.'
                )}
              </p>
            </div>
          </div>
          <div className='flex shrink-0 items-center gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              onClick={() =>
                window.open(
                  UPDATE_REPOSITORY_URL,
                  '_blank',
                  'noopener,noreferrer'
                )
              }
            >
              <ExternalLink data-icon='inline-start' className='size-3.5' />
              {t('Repository')}
            </Button>
            <Button
              type='button'
              size='sm'
              onClick={() => void handleCheck()}
              disabled={checking || active}
            >
              <RefreshCw
                data-icon='inline-start'
                className={cn('size-3.5', checking && 'animate-spin')}
              />
              {checking ? t('Checking updates...') : t('Check for updates')}
            </Button>
          </div>
        </div>

        {loading ? (
          <div className='grid gap-3 p-4 sm:grid-cols-[180px_1fr] sm:p-5'>
            <Skeleton className='h-20 rounded-md' />
            <Skeleton className='h-20 rounded-md' />
          </div>
        ) : (
          <div className='bg-border grid gap-px sm:grid-cols-[180px_1fr]'>
            <div className='bg-card p-4 sm:p-5'>
              <p className='text-muted-foreground text-xs'>
                {t('Current version')}
              </p>
              <p className='mt-1 font-mono text-xl font-semibold tabular-nums'>
                {currentVersion}
              </p>
              {capability ? (
                <Badge variant='outline' className='mt-2 font-mono text-[10px]'>
                  {capability.platform}/{capability.arch}
                </Badge>
              ) : null}
            </div>
            <div className='bg-card p-4 sm:p-5'>
              <div className='flex items-start gap-3'>
                <ShieldCheck className='text-primary mt-0.5 size-5 shrink-0' />
                <div className='min-w-0 flex-1'>
                  <p className='text-sm font-medium'>
                    {t('Update capability')}
                  </p>
                  <p className='text-muted-foreground mt-1 text-sm'>
                    {capabilityText}
                  </p>
                </div>
              </div>
              {updateState && updateState.phase !== 'idle' ? (
                <button
                  type='button'
                  className='hover:bg-muted/50 mt-3 flex w-full items-center gap-2 rounded-md border px-3 py-2 text-start transition-colors'
                  onClick={() => setProgressOpen(true)}
                >
                  <UpdatePhaseIcon phase={updateState.phase} />
                  <span className='min-w-0 flex-1 truncate text-sm'>
                    {phaseLabel(updateState.phase)}
                  </span>
                  <Badge variant='secondary' className='tabular-nums'>
                    {updateState.progress}%
                  </Badge>
                </button>
              ) : null}
            </div>
          </div>
        )}
      </section>

      <Dialog
        open={releaseOpen}
        onOpenChange={setReleaseOpen}
        title={
          release?.tag_name
            ? t('New version available: {{version}}', {
                version: release.tag_name,
              })
            : t('Release details')
        }
        description={
          release?.published_at
            ? `${t('Published')} ${formatTimestampToDate(
                new Date(release.published_at).getTime(),
                'milliseconds'
              )}`
            : undefined
        }
        contentClassName='max-h-[80vh]'
        bodyClassName='space-y-4'
        footer={
          <>
            <Button variant='secondary' onClick={() => setReleaseOpen(false)}>
              {t('Close')}
            </Button>
            {release?.html_url ? (
              <Button
                variant='outline'
                onClick={() =>
                  window.open(release.html_url, '_blank', 'noopener,noreferrer')
                }
              >
                <ExternalLink data-icon='inline-start' className='size-4' />
                {t('Open release')}
              </Button>
            ) : null}
            {release?.installable ? (
              <Button onClick={() => setConfirmOpen(true)}>
                <Download data-icon='inline-start' className='size-4' />
                {t('Install online')}
              </Button>
            ) : null}
          </>
        }
      >
        {!release?.installable && release?.reason ? (
          <div className='flex gap-3 rounded-md border border-amber-500/40 bg-amber-500/10 p-3 text-sm'>
            <TriangleAlert className='mt-0.5 size-4 shrink-0 text-amber-500' />
            <span>{reasonLabel(release.reason)}</span>
          </div>
        ) : null}
        {release?.asset_name ? (
          <div className='bg-muted rounded-md px-3 py-2 font-mono text-xs'>
            {t('Verified update package')}: {release.asset_name}
          </div>
        ) : null}
        {release?.body ? (
          <Markdown>{release.body}</Markdown>
        ) : (
          <p className='text-muted-foreground text-sm'>
            {t('No release notes provided.')}
          </p>
        )}
      </Dialog>

      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Install this update now?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'The package will be verified before installation. The service will wait for active requests and validate the new version. Automatic rollback is used only when it can be performed safely without competing with the service manager.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={installing}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={() => void handleInstall()}
              disabled={installing}
            >
              {installing ? (
                <Loader2
                  data-icon='inline-start'
                  className='size-4 animate-spin'
                />
              ) : (
                <Download data-icon='inline-start' className='size-4' />
              )}
              {t('Start update')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <Dialog
        open={progressOpen}
        onOpenChange={(open) => {
          if (!active) setProgressOpen(open)
        }}
        showCloseButton={!active}
        title={t('Online update')}
        description={
          updateState?.target_version
            ? t('Updating from {{from}} to {{to}}', {
                from: updateState.current_version,
                to: updateState.target_version,
              })
            : undefined
        }
        footer={
          !active ? (
            <>
              <Button
                variant='secondary'
                onClick={() => setProgressOpen(false)}
              >
                {t('Close')}
              </Button>
              {updateState?.phase === 'succeeded' ? (
                <Button onClick={() => window.location.reload()}>
                  <RefreshCw data-icon='inline-start' className='size-4' />
                  {t('Reload page')}
                </Button>
              ) : null}
              {updateState?.phase === 'failed' ||
              updateState?.phase === 'rolled_back' ? (
                <Button variant='outline' onClick={() => void handleCheck()}>
                  <RotateCcw data-icon='inline-start' className='size-4' />
                  {t('Check again')}
                </Button>
              ) : null}
            </>
          ) : undefined
        }
      >
        <div className='space-y-5'>
          <div className='flex items-center gap-3'>
            <UpdatePhaseIcon phase={updateState?.phase} large />
            <div>
              <p className='font-medium'>{phaseLabel(updateState?.phase)}</p>
              {reconnecting ? (
                <p className='text-muted-foreground text-sm'>
                  {t('Connection interrupted during restart. Reconnecting...')}
                </p>
              ) : null}
            </div>
          </div>
          <Progress value={updateState?.progress || 0} />
          <div className='text-muted-foreground flex justify-between gap-4 text-xs'>
            <span>
              {t('Do not close the server process during replacement.')}
            </span>
            <span className='shrink-0 tabular-nums'>
              {updateState?.progress || 0}%
            </span>
          </div>
          {updateState?.error_code ? (
            <div className='bg-destructive/10 text-destructive rounded-md p-3 font-mono text-xs'>
              {t('Error code')}: {updateState.error_code}
            </div>
          ) : null}
        </div>
      </Dialog>
    </>
  )
}
