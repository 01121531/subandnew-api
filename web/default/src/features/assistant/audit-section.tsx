/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { useQuery } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import {
  Activity,
  ChevronDown,
  ChevronUp,
  CircleAlert,
  Clock3,
  Coins,
  LockKeyhole,
  RefreshCw,
  Wrench,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { ErrorState } from '@/components/error-state'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Skeleton } from '@/components/ui/skeleton'
import { TitledCard } from '@/components/ui/titled-card'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { useAuthStore } from '@/stores/auth-store'

import { getAssistantRun, listAssistantRuns } from './api'
import type {
  AssistantRun,
  AssistantRunStatus,
  AssistantToolCall,
  AssistantToolCallStatus,
  AssistantToolRisk,
} from './types'

const PAGE_SIZE = 10

function runStatusLabel(status: AssistantRunStatus, t: TFunction) {
  const labels: Record<AssistantRunStatus, string> = {
    pending: t('Pending'),
    running: t('Running'),
    succeeded: t('Succeeded'),
    failed: t('Failed'),
    cancelled: t('Cancelled'),
  }
  return labels[status]
}

function runStatusVariant(status: AssistantRunStatus) {
  if (status === 'succeeded') return 'secondary' as const
  if (status === 'failed') return 'destructive' as const
  return 'outline' as const
}

function toolStatusLabel(status: AssistantToolCallStatus, t: TFunction) {
  const labels: Record<AssistantToolCallStatus, string> = {
    pending: t('Pending'),
    running: t('Running'),
    succeeded: t('Succeeded'),
    failed: t('Failed'),
    denied: t('Denied'),
  }
  return labels[status]
}

function riskLabel(risk: AssistantToolRisk, t: TFunction) {
  const labels: Record<AssistantToolRisk, string> = {
    low: t('Low risk'),
    medium: t('Medium risk'),
    high: t('High risk'),
    critical: t('Critical risk'),
  }
  return labels[risk]
}

function formatDuration(run: AssistantRun, t: TFunction) {
  if (run.started_at <= 0 || run.finished_at <= 0) {
    return t('In progress')
  }
  const milliseconds = Math.max(0, run.finished_at - run.started_at) * 1000
  if (milliseconds < 1000) {
    return t('{{count}} ms', { count: milliseconds })
  }
  return t('{{count}} s', { count: (milliseconds / 1000).toFixed(1) })
}

function configuredTimeoutSeconds(run: AssistantRun) {
  if (run.deadline_at <= 0 || run.started_at <= 0) return 0
  return Math.max(0, run.deadline_at - run.started_at)
}

function providerRetrySummary(run: AssistantRun, t: TFunction) {
  if (run.request_timeout_seconds <= 0) return t('Retry metrics unavailable')
  if (!run.retried_before_first_byte) return t('No provider retry')
  return t('{{count}} retries before first response', {
    count: run.provider_retry_count,
  })
}

function failureReason(run: AssistantRun, t: TFunction) {
  const timeout = configuredTimeoutSeconds(run)
  const reasons: Record<string, string> = {
    provider_authentication_failed: t(
      'The model provider rejected authentication.'
    ),
    provider_timeout: t('The model provider request timed out.'),
    provider_rate_limited: t('The model provider rate limit was reached.'),
    provider_unavailable: t('The model provider is temporarily unavailable.'),
    provider_rejected_request: t('The model provider rejected the request.'),
    provider_http_error: t('The model provider returned an HTTP error.'),
    provider_connection_failed: t('Could not connect to the model provider.'),
    provider_stream_error: t('The model response stream ended unexpectedly.'),
    invalid_model_response: t('The model returned an invalid response.'),
    step_limit_reached: t('The assistant reached the maximum reasoning steps.'),
    tool_call_limit_reached: t('The assistant reached the maximum tool calls.'),
    tool_authorization_denied: t('A tool call was denied by permissions.'),
    tool_invalid_arguments: t('A tool call used invalid arguments.'),
    tool_not_found: t('The requested assistant tool was not found.'),
    tool_execution_failed: t('An assistant tool failed to execute.'),
    message_persistence_failed: t('The assistant result could not be saved.'),
    message_delivery_failed: t('The assistant reply could not be delivered.'),
    configuration_failed: t('The assistant configuration is invalid.'),
    runner_configuration_invalid: t(
      'The assistant runner configuration is invalid.'
    ),
    run_cancelled: t('The assistant run was cancelled.'),
    unknown_failure: t('The assistant run failed for an unknown reason.'),
  }
  if (
    run.error_reason_code === 'run_timeout' ||
    run.error_code === 'agent_run_timeout'
  ) {
    return timeout > 0
      ? t('The assistant did not finish within {{seconds}} seconds.', {
          seconds: timeout,
        })
      : t('The assistant run timed out.')
  }
  return (
    reasons[run.error_reason_code ?? ''] ??
    t('This historical run did not save a detailed reason.')
  )
}

function failureStageLabel(stage: string | undefined, t: TFunction) {
  const stages: Record<string, string> = {
    model_request: t('Model request'),
    model_stream: t('Model response stream'),
    model_response: t('Model response validation'),
    tool_execution: t('Tool execution'),
    runner: t('Assistant runner'),
    message_persistence: t('Message persistence'),
    message_delivery: t('Message delivery'),
    configuration: t('Configuration'),
    unknown: t('Unknown stage'),
  }
  return stages[stage ?? ''] ?? t('Unknown stage')
}

function FailureDiagnosis(props: { run: AssistantRun }) {
  const { t } = useTranslation()
  if (props.run.status !== 'failed') return null

  return (
    <section className='border-destructive/30 bg-destructive/5 mt-3 rounded-lg border p-3'>
      <div className='flex flex-wrap items-start justify-between gap-2'>
        <div>
          <p className='text-destructive text-sm font-medium'>
            {t('Failure diagnosis')}
          </p>
          <p className='text-muted-foreground mt-1 text-sm'>
            {failureReason(props.run, t)}
          </p>
        </div>
        {props.run.error_detail && (
          <CopyButton
            value={props.run.error_detail}
            variant='outline'
            size='sm'
            className='min-h-11 px-3'
            tooltip={t('Copy error details')}
            aria-label={t('Copy error details')}
          >
            {t('Copy details')}
          </CopyButton>
        )}
      </div>
      <dl className='mt-3 grid gap-2 text-xs sm:grid-cols-2 lg:grid-cols-4'>
        <div>
          <dt className='text-muted-foreground'>{t('Failure stage')}</dt>
          <dd className='mt-0.5'>
            {failureStageLabel(props.run.error_stage, t)}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Reason code')}</dt>
          <dd className='mt-0.5 font-mono break-all'>
            {props.run.error_reason_code || t('Unavailable')}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Provider HTTP status')}</dt>
          <dd className='mt-0.5 font-mono'>
            {props.run.provider_status_code || t('Unavailable')}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Provider error code')}</dt>
          <dd className='mt-0.5 font-mono break-all'>
            {props.run.provider_error_code || t('Unavailable')}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>
            {t('Model request timeout')}
          </dt>
          <dd className='mt-0.5'>
            {props.run.request_timeout_seconds > 0
              ? t('{{count}} seconds', {
                  count: props.run.request_timeout_seconds,
                })
              : t('Unavailable')}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Total run timeout')}</dt>
          <dd className='mt-0.5'>
            {configuredTimeoutSeconds(props.run) > 0
              ? t('{{count}} seconds', {
                  count: configuredTimeoutSeconds(props.run),
                })
              : t('Unavailable')}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Model requests')}</dt>
          <dd className='mt-0.5'>
            {props.run.request_timeout_seconds > 0
              ? props.run.model_request_count
              : t('Unavailable')}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>
            {t('Retries before first response')}
          </dt>
          <dd className='mt-0.5'>
            {props.run.request_timeout_seconds > 0
              ? props.run.provider_retry_count
              : t('Unavailable')}
          </dd>
        </div>
      </dl>
      <div className='mt-3'>
        <p className='text-muted-foreground text-xs'>{t('Error details')}</p>
        <pre className='bg-background mt-1 max-h-72 overflow-auto rounded-md border p-3 font-mono text-xs break-all whitespace-pre-wrap'>
          {props.run.error_detail ||
            t('This historical run did not save a detailed reason.')}
        </pre>
        {props.run.error_detail_truncated && (
          <p className='text-warning mt-1 text-xs'>
            {t('The error details were truncated at 64 KiB.')}
          </p>
        )}
      </div>
    </section>
  )
}

function CacheUsage(props: { run: AssistantRun }) {
  const { t } = useTranslation()
  const observedValue = Number(props.run.cache_observed_input_tokens)
  const observed = Number.isFinite(observedValue)
    ? Math.max(0, observedValue)
    : 0
  if (observed === 0) {
    return (
      <p className='text-muted-foreground mt-1 text-xs'>
        {t('Cache metrics unavailable')}
      </p>
    )
  }

  const cachedValue = Number(props.run.cached_input_tokens)
  const cached = Math.min(
    observed,
    Number.isFinite(cachedValue) ? Math.max(0, cachedValue) : 0
  )
  const uncached = observed - cached
  const rate = (cached / observed) * 100
  const inputValue = Number(props.run.input_tokens)
  const inputTokens = Number.isFinite(inputValue) ? Math.max(0, inputValue) : 0
  const complete = observed >= inputTokens

  return (
    <div className='mt-1 space-y-1 text-xs'>
      <div className='flex flex-wrap items-center gap-1.5'>
        <span className='text-muted-foreground'>
          {t('{{cached}} cached / {{observed}} observed', {
            cached: cached.toLocaleString(),
            observed: observed.toLocaleString(),
          })}
        </span>
        <Badge variant='outline' className='h-5 px-1.5 text-[10px]'>
          {complete ? t('Complete report') : t('Partial report')}
        </Badge>
      </div>
      <p className='text-muted-foreground'>
        {t('{{rate}} hit rate · {{uncached}} uncached', {
          rate: `${rate.toFixed(1)}%`,
          uncached: uncached.toLocaleString(),
        })}
      </p>
    </div>
  )
}

function argumentHash(redacted?: string) {
  if (!redacted) return ''
  return redacted.match(/[a-fA-F0-9]{64}/)?.[0] ?? ''
}

function ToolTrace(props: { call: AssistantToolCall }) {
  const { t } = useTranslation()
  const hash = argumentHash(props.call.arguments_redacted)

  return (
    <li className='bg-muted/30 rounded-lg border p-3'>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex min-w-0 items-center gap-2'>
          <span className='bg-background flex size-8 shrink-0 items-center justify-center rounded-lg border'>
            <Wrench className='size-4' aria-hidden='true' />
          </span>
          <div className='min-w-0'>
            <p className='truncate font-medium'>{props.call.tool}</p>
            <p className='text-muted-foreground text-xs'>
              {t('Step {{sequence}} · {{duration}} ms', {
                sequence: props.call.sequence,
                duration: props.call.latency_ms,
              })}
            </p>
          </div>
        </div>
        <div className='flex flex-wrap gap-1.5'>
          <Badge
            variant={props.call.status === 'failed' ? 'destructive' : 'outline'}
          >
            {toolStatusLabel(props.call.status, t)}
          </Badge>
          <Badge variant='outline'>{riskLabel(props.call.risk, t)}</Badge>
        </div>
      </div>
      <dl className='mt-3 grid gap-2 text-xs sm:grid-cols-2'>
        <div>
          <dt className='text-muted-foreground'>{t('Permission')}</dt>
          <dd className='mt-0.5 font-mono break-all'>
            {props.call.permission}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Argument hash')}</dt>
          <dd className='mt-0.5 font-mono break-all'>
            {hash || t('Unavailable')}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Result digest')}</dt>
          <dd className='mt-0.5 font-mono break-all'>
            {props.call.result_digest || t('Unavailable')}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground'>{t('Error code')}</dt>
          <dd className='mt-0.5 font-mono break-all'>
            {props.call.error_code || t('None')}
          </dd>
        </div>
      </dl>
      {props.call.error_detail && (
        <div className='mt-3'>
          <div className='flex items-center justify-between gap-2'>
            <p className='text-muted-foreground text-xs'>
              {t('Error details')}
            </p>
            <CopyButton
              value={props.call.error_detail}
              tooltip={t('Copy error details')}
              aria-label={t('Copy error details')}
            />
          </div>
          <pre className='bg-background mt-1 max-h-56 overflow-auto rounded-md border p-3 font-mono text-xs break-all whitespace-pre-wrap'>
            {props.call.error_detail}
          </pre>
          {props.call.error_detail_truncated && (
            <p className='text-warning mt-1 text-xs'>
              {t('The error details were truncated at 64 KiB.')}
            </p>
          )}
        </div>
      )}
    </li>
  )
}

function RunTrace(props: { runId: string }) {
  const { t } = useTranslation()
  const detailQuery = useQuery({
    queryKey: ['assistant', 'runs', props.runId],
    queryFn: () => getAssistantRun(props.runId),
  })

  if (detailQuery.isLoading) {
    return <Skeleton className='mt-3 h-28' aria-label={t('Loading...')} />
  }
  if (detailQuery.isError) {
    return (
      <ErrorState
        className='mt-3 min-h-32'
        title={t('Could not load tool trace')}
        description={t('Check the server connection and try again.')}
        onRetry={() => detailQuery.refetch()}
      />
    )
  }
  if (!detailQuery.data) {
    return null
  }
  if (detailQuery.data.tool_calls.length === 0) {
    return (
      <div>
        <FailureDiagnosis run={detailQuery.data.run} />
        <p className='text-muted-foreground mt-3 rounded-lg border border-dashed p-4 text-center text-sm'>
          {t('This run did not call any tools.')}
        </p>
      </div>
    )
  }

  return (
    <div>
      <FailureDiagnosis run={detailQuery.data.run} />
      <ul className='mt-3 space-y-2'>
        {detailQuery.data.tool_calls.map((call) => (
          <ToolTrace key={call.id} call={call} />
        ))}
      </ul>
    </div>
  )
}

function RunRow(props: {
  run: AssistantRun
  expanded: boolean
  onToggle: () => void
}) {
  const { t } = useTranslation()

  return (
    <li className='bg-muted/20 rounded-xl border p-4'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between'>
        <div className='min-w-0'>
          <div className='flex flex-wrap items-center gap-2'>
            <p className='truncate font-medium'>
              {props.run.model || t('Unknown model')}
            </p>
            <Badge variant={runStatusVariant(props.run.status)}>
              {runStatusLabel(props.run.status, t)}
            </Badge>
          </div>
          <p className='text-muted-foreground mt-1 truncate font-mono text-xs'>
            {props.run.run_id}
          </p>
        </div>
        <Button
          variant='outline'
          className='min-h-11 w-full sm:w-auto'
          aria-expanded={props.expanded}
          aria-controls={`assistant-run-trace-${props.run.id}`}
          onClick={props.onToggle}
        >
          {props.expanded ? <ChevronUp /> : <ChevronDown />}
          {props.expanded ? t('Hide trace') : t('View trace')}
        </Button>
      </div>

      <dl className='mt-4 grid grid-cols-2 gap-3 text-sm lg:grid-cols-6'>
        <div>
          <dt className='text-muted-foreground flex items-center gap-1.5 text-xs'>
            <Clock3 className='size-3.5' aria-hidden='true' />
            {t('Duration')}
          </dt>
          <dd className='mt-1 font-medium'>{formatDuration(props.run, t)}</dd>
        </div>
        <div>
          <dt className='text-muted-foreground flex items-center gap-1.5 text-xs'>
            <Coins className='size-3.5' aria-hidden='true' />
            {t('Tokens')}
          </dt>
          <dd className='mt-1 font-medium'>
            {props.run.total_tokens.toLocaleString()}
          </dd>
          <dd className='text-muted-foreground text-xs'>
            {t('{{input}} in / {{output}} out', {
              input: props.run.input_tokens,
              output: props.run.output_tokens,
            })}
          </dd>
          <CacheUsage run={props.run} />
        </div>
        <div>
          <dt className='text-muted-foreground text-xs'>{t('Started')}</dt>
          <dd className='mt-1 text-xs'>
            {props.run.started_at > 0
              ? new Date(props.run.started_at * 1000).toLocaleString()
              : t('Not started')}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground text-xs'>
            {t('Model requests')}
          </dt>
          <dd className='mt-1 font-medium'>
            {props.run.request_timeout_seconds > 0
              ? props.run.model_request_count
              : t('Unavailable')}
          </dd>
          <dd className='text-muted-foreground text-xs'>
            {providerRetrySummary(props.run, t)}
          </dd>
        </div>
        <div>
          <dt className='text-muted-foreground flex items-center gap-1.5 text-xs'>
            <CircleAlert className='size-3.5' aria-hidden='true' />
            {t('Error code')}
          </dt>
          <dd className='mt-1 font-mono text-xs break-all'>
            {props.run.error_code || t('None')}
          </dd>
        </div>
      </dl>

      {props.run.status === 'failed' && (
        <div className='border-destructive/25 bg-destructive/5 mt-3 rounded-lg border px-3 py-2'>
          <p className='text-destructive text-xs font-medium'>
            {t('Failure reason')}
          </p>
          <p className='mt-1 text-sm break-words'>
            {failureReason(props.run, t)}
          </p>
        </div>
      )}

      {props.expanded && (
        <div id={`assistant-run-trace-${props.run.id}`}>
          <RunTrace runId={props.run.run_id} />
        </div>
      )}
    </li>
  )
}

export function AssistantAuditSection() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const canAudit = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.ASSISTANT,
    ADMIN_PERMISSION_ACTIONS.AUDIT
  )
  const [page, setPage] = useState(1)
  const [status, setStatus] = useState<AssistantRunStatus | ''>('')
  const [expandedRunId, setExpandedRunId] = useState<string | null>(null)
  const runsQuery = useQuery({
    queryKey: ['assistant', 'runs', { page, status }],
    queryFn: ({ signal }) =>
      listAssistantRuns({ page, pageSize: PAGE_SIZE, status, signal }),
    enabled: canAudit,
    refetchInterval: (query) =>
      query.state.data?.items.some((run) =>
        ['pending', 'running'].includes(run.status)
      )
        ? 5_000
        : false,
  })
  const totalPages = Math.max(
    1,
    Math.ceil((runsQuery.data?.total ?? 0) / PAGE_SIZE)
  )

  return (
    <TitledCard
      title={t('Run audit')}
      description={t(
        'Inspect model usage, failures, latency, and redacted tool traces.'
      )}
      icon={<Activity />}
      iconTone='info'
      action={
        <div className='flex w-full items-end gap-2 sm:w-auto'>
          <div className='min-w-0 flex-1 space-y-1.5 sm:w-48'>
            <Label htmlFor='assistant-run-status' className='sr-only'>
              {t('Filter by status')}
            </Label>
            <NativeSelect
              id='assistant-run-status'
              className='w-full [&_select]:h-11'
              value={status}
              onChange={(event) => {
                setStatus(event.target.value as AssistantRunStatus | '')
                setPage(1)
                setExpandedRunId(null)
              }}
            >
              <NativeSelectOption value=''>
                {t('All statuses')}
              </NativeSelectOption>
              <NativeSelectOption value='pending'>
                {t('Pending')}
              </NativeSelectOption>
              <NativeSelectOption value='running'>
                {t('Running')}
              </NativeSelectOption>
              <NativeSelectOption value='succeeded'>
                {t('Succeeded')}
              </NativeSelectOption>
              <NativeSelectOption value='failed'>
                {t('Failed')}
              </NativeSelectOption>
              <NativeSelectOption value='cancelled'>
                {t('Cancelled')}
              </NativeSelectOption>
            </NativeSelect>
          </div>
          <Button
            variant='outline'
            size='icon'
            className='size-11 shrink-0'
            aria-label={t('Refresh')}
            disabled={runsQuery.isFetching}
            onClick={() => runsQuery.refetch()}
          >
            <RefreshCw className={runsQuery.isFetching ? 'animate-spin' : ''} />
          </Button>
        </div>
      }
    >
      {!canAudit && (
        <Empty className='min-h-48 border'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <LockKeyhole />
            </EmptyMedia>
            <EmptyTitle>{t('Run audit permission required')}</EmptyTitle>
            <EmptyDescription>
              {t(
                'Ask an administrator to grant the AI assistant audit permission.'
              )}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
      {runsQuery.isLoading && (
        <div className='space-y-3' aria-label={t('Loading...')}>
          <Skeleton className='h-40' />
          <Skeleton className='h-40' />
        </div>
      )}
      {runsQuery.isError && (
        <ErrorState
          className='min-h-48'
          title={t('Could not load assistant runs')}
          description={t('Check the server connection and try again.')}
          onRetry={() => runsQuery.refetch()}
        />
      )}
      {runsQuery.isSuccess && runsQuery.data.items.length === 0 && (
        <Empty className='min-h-48 border'>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              <Activity />
            </EmptyMedia>
            <EmptyTitle>{t('No assistant runs found')}</EmptyTitle>
            <EmptyDescription>
              {status
                ? t('No runs match the selected status.')
                : t(
                    'Runs will appear here after the assistant handles a message.'
                  )}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}
      {runsQuery.isSuccess && runsQuery.data.items.length > 0 && (
        <>
          <ul className='space-y-3'>
            {runsQuery.data.items.map((run) => (
              <RunRow
                key={run.id}
                run={run}
                expanded={expandedRunId === run.run_id}
                onToggle={() =>
                  setExpandedRunId((current) =>
                    current === run.run_id ? null : run.run_id
                  )
                }
              />
            ))}
          </ul>
          <div className='mt-4 flex flex-col gap-2 border-t pt-4 sm:flex-row sm:items-center sm:justify-between'>
            <p className='text-muted-foreground text-center text-sm sm:text-left'>
              {t('Page {{page}} of {{total}} · {{count}} runs', {
                page,
                total: totalPages,
                count: runsQuery.data.total,
              })}
            </p>
            <div className='grid grid-cols-2 gap-2'>
              <Button
                variant='outline'
                className='min-h-11'
                disabled={page <= 1}
                onClick={() => {
                  setPage((current) => current - 1)
                  setExpandedRunId(null)
                }}
              >
                {t('Previous')}
              </Button>
              <Button
                variant='outline'
                className='min-h-11'
                disabled={page >= totalPages}
                onClick={() => {
                  setPage((current) => current + 1)
                  setExpandedRunId(null)
                }}
              >
                {t('Next')}
              </Button>
            </div>
          </div>
        </>
      )}
    </TitledCard>
  )
}
