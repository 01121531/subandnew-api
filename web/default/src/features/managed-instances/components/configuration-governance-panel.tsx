import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  CheckCircle2,
  FileDiff,
  LoaderCircle,
  Pencil,
  Plus,
  RefreshCw,
  Save,
  Trash2,
  TriangleAlert,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import {
  createManagedConfigTemplate,
  deleteManagedConfigTemplate,
  executeManagedInstanceConfigApply,
  getManagedConfigSchemas,
  getManagedConfigTemplates,
  getManagedInstanceConfig,
  getManagedInstanceConfigOperation,
  planManagedInstanceConfigApply,
  refreshManagedInstanceConfig,
  setManagedInstanceConfig,
  updateManagedConfigTemplate,
} from '../api'
import { formatTimestamp } from '../lib'
import type {
  ApiResponse,
  ManagedConfigFieldSchema,
  ManagedConfigMode,
  ManagedConfigPreview,
  ManagedConfigSchema,
  ManagedConfigTemplate,
  ManagedConfigTemplateInput,
  ManagedInstance,
  ManagedInstanceOperation,
  ManagedInstanceOperationStatus,
} from '../types'

const activeStatuses = new Set<ManagedInstanceOperationStatus>([
  'planned',
  'queued',
  'running',
])

export function ConfigurationGovernancePanel({
  instance,
}: {
  instance: ManagedInstance
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const user = useAuthStore((state) => state.auth.user)
  const canApply = hasPermission(
    user,
    ADMIN_PERMISSION_RESOURCES.MANAGED_TEMPLATE,
    ADMIN_PERMISSION_ACTIONS.APPLY
  )
  const [selectedTemplateId, setSelectedTemplateId] = useState(0)
  const [selectedMode, setSelectedMode] = useState<ManagedConfigMode>('audit')
  const [editingTemplate, setEditingTemplate] = useState<
    ManagedConfigTemplate | null | undefined
  >(undefined)
  const [deletingTemplate, setDeletingTemplate] =
    useState<ManagedConfigTemplate | null>(null)
  const [preview, setPreview] = useState<ManagedConfigPreview | null>(null)
  const [planned, setPlanned] = useState<{
    operation: ManagedInstanceOperation
    key: string
  } | null>(null)
  const [confirmed, setConfirmed] = useState(false)
  const [operationId, setOperationId] = useState<string | null>(null)
  const terminalNotification = useRef('')

  const schemasQuery = useQuery({
    queryKey: ['managed-config-schemas'],
    queryFn: getManagedConfigSchemas,
  })
  const templatesQuery = useQuery({
    queryKey: ['managed-config-templates', instance.kind],
    queryFn: () => getManagedConfigTemplates(instance.kind),
  })
  const bindingQuery = useQuery({
    queryKey: ['managed-instance-config', instance.id],
    queryFn: () => getManagedInstanceConfig(instance.id),
  })
  const operationQuery = useQuery({
    queryKey: ['managed-config-operation', instance.id, operationId],
    queryFn: () =>
      getManagedInstanceConfigOperation(instance.id, operationId ?? ''),
    enabled: Boolean(operationId),
    refetchInterval: (query) => {
      const response = query.state.data as
        | ApiResponse<ManagedInstanceOperation>
        | undefined
      return !response || activeStatuses.has(response.data.status)
        ? 1_500
        : false
    },
  })
  const templates = useMemo(
    () => templatesQuery.data?.data.items ?? [],
    [templatesQuery.data]
  )
  const schema = schemasQuery.data?.data.find(
    (candidate) => candidate.kind === instance.kind
  )
  const binding = bindingQuery.data?.data

  useEffect(() => {
    if (!binding) return
    setSelectedTemplateId(binding.template_id)
    setSelectedMode(binding.mode)
  }, [binding])

  const bindingMutation = useMutation({
    mutationFn: () =>
      setManagedInstanceConfig(instance.id, {
        template_id: selectedTemplateId,
        mode: selectedMode,
      }),
    onSuccess: (response) => {
      if (!response.success) return
      setPreview(null)
      setPlanned(null)
      toast.success(t('Configuration binding saved'))
      void bindingQuery.refetch()
    },
  })
  const refreshMutation = useMutation({
    mutationFn: () => refreshManagedInstanceConfig(instance.id),
    onSuccess: (response) => {
      if (!response.success) return
      setPreview(response.data)
      setPlanned(null)
      void bindingQuery.refetch()
    },
  })
  const planMutation = useMutation({
    mutationFn: (key: string) =>
      planManagedInstanceConfigApply(instance.id, {
        expected_observed_hash: preview?.observed_hash ?? '',
        idempotency_key: key,
      }),
    onSuccess: (response, key) => {
      if (!response.success) return
      setConfirmed(false)
      setPlanned({ operation: response.data, key })
    },
  })
  const executeMutation = useMutation({
    mutationFn: () => {
      if (!planned) throw new Error('missing config apply plan')
      return executeManagedInstanceConfigApply(instance.id, {
        operation_id: planned.operation.operation_id,
        idempotency_key: planned.key,
      })
    },
    onSuccess: (response) => {
      if (!response.success) return
      setOperationId(response.data.operation.operation_id)
      setPlanned(null)
      setConfirmed(false)
      toast.success(t('Configuration apply queued'))
    },
  })
  const templateMutation = useMutation({
    mutationFn: (input: ManagedConfigTemplateInput) =>
      editingTemplate
        ? updateManagedConfigTemplate(editingTemplate.id, input)
        : createManagedConfigTemplate(input),
    onSuccess: (response) => {
      if (!response.success) return
      setEditingTemplate(undefined)
      toast.success(t('Configuration template saved'))
      void templatesQuery.refetch()
    },
  })
  const deleteMutation = useMutation({
    mutationFn: (templateId: number) => deleteManagedConfigTemplate(templateId),
    onSuccess: (response) => {
      if (!response.success) return
      setDeletingTemplate(null)
      toast.success(t('Configuration template deleted'))
      void templatesQuery.refetch()
    },
  })

  const operation = operationQuery.data?.data
  useEffect(() => {
    if (!operation || activeStatuses.has(operation.status)) return
    if (terminalNotification.current === operation.operation_id) return
    terminalNotification.current = operation.operation_id
    if (operation.status === 'succeeded') {
      toast.success(t('Configuration verified after apply'))
    } else if (operation.status === 'unknown') {
      toast.warning(t('Configuration result needs manual reconciliation'))
    } else if (operation.error_code === 'config_apply_failed_rolled_back') {
      toast.warning(t('Configuration apply failed and was rolled back'))
    } else {
      toast.error(
        t('Configuration apply failed: {{code}}', {
          code: operation.error_code || 'operation_failed',
        })
      )
    }
    setPreview(null)
    void bindingQuery.refetch()
    void queryClient.invalidateQueries({
      queryKey: ['managed-instance-audits', instance.id],
    })
  }, [bindingQuery, instance.id, operation, queryClient, t])

  const selectedTemplate = templates.find(
    (template) => template.id === selectedTemplateId
  )
  const canPlan = Boolean(
    canApply &&
    preview?.drifted &&
    binding?.mode === 'enforce' &&
    instance.management_mode === 'enforce' &&
    !operationQuery.isFetching
  )

  return (
    <section className='min-w-0 rounded-lg border'>
      <header className='flex flex-wrap items-start justify-between gap-3 border-b px-4 py-3'>
        <div>
          <h2 className='text-sm font-semibold'>
            {t('Configuration governance')}
          </h2>
          <p className='text-muted-foreground mt-0.5 text-xs'>
            {t(
              'Versioned whitelist templates with drift detection and verified apply.'
            )}
          </p>
        </div>
        {binding && <DriftBadge status={binding.drift_status} />}
      </header>

      <div className='grid gap-4 p-4'>
        <div className='grid gap-3 lg:grid-cols-[minmax(0,1fr)_180px_auto] lg:items-end'>
          <div className='grid gap-1.5'>
            <Label htmlFor={`config-template-${instance.id}`}>
              {t('Template')}
            </Label>
            <div className='flex min-w-0 gap-1.5'>
              <NativeSelect
                id={`config-template-${instance.id}`}
                className='min-w-0 flex-1'
                value={selectedTemplateId || ''}
                disabled={!canApply}
                onChange={(event) =>
                  setSelectedTemplateId(Number(event.target.value))
                }
              >
                <NativeSelectOption value=''>
                  {t('Select a template')}
                </NativeSelectOption>
                {templates.map((template) => (
                  <NativeSelectOption key={template.id} value={template.id}>
                    {template.name}
                  </NativeSelectOption>
                ))}
              </NativeSelect>
              {canApply && schema && (
                <>
                  <IconAction
                    label={t('New template')}
                    onClick={() => setEditingTemplate(null)}
                  >
                    <Plus />
                  </IconAction>
                  <IconAction
                    label={t('Edit template')}
                    disabled={!selectedTemplate}
                    onClick={() => setEditingTemplate(selectedTemplate)}
                  >
                    <Pencil />
                  </IconAction>
                  <IconAction
                    label={t('Delete template')}
                    disabled={!selectedTemplate}
                    onClick={() =>
                      setDeletingTemplate(selectedTemplate ?? null)
                    }
                  >
                    <Trash2 />
                  </IconAction>
                </>
              )}
            </div>
          </div>
          <div className='grid gap-1.5'>
            <Label htmlFor={`config-mode-${instance.id}`}>{t('Mode')}</Label>
            <NativeSelect
              id={`config-mode-${instance.id}`}
              className='w-full'
              value={selectedMode}
              disabled={!canApply}
              onChange={(event) =>
                setSelectedMode(event.target.value as ManagedConfigMode)
              }
            >
              <NativeSelectOption value='disabled'>
                {t('Disabled')}
              </NativeSelectOption>
              <NativeSelectOption value='audit'>
                {t('Audit only')}
              </NativeSelectOption>
              <NativeSelectOption value='enforce'>
                {t('Enforce')}
              </NativeSelectOption>
            </NativeSelect>
          </div>
          {canApply && (
            <Button
              variant='outline'
              disabled={!selectedTemplateId || bindingMutation.isPending}
              onClick={() => bindingMutation.mutate()}
            >
              {bindingMutation.isPending ? (
                <LoaderCircle className='animate-spin' />
              ) : (
                <Save />
              )}
              {t('Save binding')}
            </Button>
          )}
        </div>

        {binding ? (
          <div className='grid gap-3 border-t pt-4'>
            <div className='flex flex-wrap items-center justify-between gap-3'>
              <div className='text-muted-foreground text-xs'>
                {t('Last checked')}: {formatTimestamp(binding.last_checked_at)}
                {binding.last_applied_at > 0 && (
                  <>
                    {' '}
                    / {t('Last applied')}:{' '}
                    {formatTimestamp(binding.last_applied_at)}
                  </>
                )}
              </div>
              <Button
                size='sm'
                variant='outline'
                disabled={
                  binding.mode === 'disabled' || refreshMutation.isPending
                }
                onClick={() => refreshMutation.mutate()}
              >
                <RefreshCw
                  className={cn(refreshMutation.isPending && 'animate-spin')}
                />
                {t('Refresh drift')}
              </Button>
            </div>

            {preview && <DiffTable preview={preview} />}

            {preview?.drifted && canApply && (
              <div className='flex flex-wrap items-center justify-between gap-3 border-t pt-4'>
                <div className='text-muted-foreground max-w-2xl text-xs'>
                  {binding.mode !== 'enforce' ||
                  instance.management_mode !== 'enforce'
                    ? t(
                        'Both the binding and instance must use enforce mode before remote changes are allowed.'
                      )
                    : t(
                        'Planning reads the remote whitelist again and locks the observed hash for execution.'
                      )}
                </div>
                <Button
                  size='sm'
                  disabled={!canPlan || planMutation.isPending}
                  onClick={() => planMutation.mutate(createIdempotencyKey())}
                >
                  <FileDiff />
                  {t('Review apply')}
                </Button>
              </div>
            )}

            {planned && (
              <div className='grid gap-3 border-t pt-4'>
                <div className='flex items-center gap-2 text-sm font-medium'>
                  <TriangleAlert className='size-4 text-amber-600' />
                  {t(
                    '{{count}} configuration changes are ready for execution.',
                    {
                      count: planned.operation.plan.target_count,
                    }
                  )}
                </div>
                <Label className='flex items-start gap-2 font-normal'>
                  <Checkbox
                    checked={confirmed}
                    onCheckedChange={(checked) =>
                      setConfirmed(checked === true)
                    }
                  />
                  <span>
                    {t('I reviewed the target values and rollback material.')}
                  </span>
                </Label>
                <div>
                  <Button
                    size='sm'
                    disabled={!confirmed || executeMutation.isPending}
                    onClick={() => executeMutation.mutate()}
                  >
                    {executeMutation.isPending ? (
                      <LoaderCircle className='animate-spin' />
                    ) : (
                      <CheckCircle2 />
                    )}
                    {t('Execute configuration apply')}
                  </Button>
                </div>
              </div>
            )}

            {operation && <OperationStatus operation={operation} />}
          </div>
        ) : (
          <p className='text-muted-foreground border-t pt-4 text-sm'>
            {t(
              'No template is bound. Audit mode is the recommended starting point.'
            )}
          </p>
        )}
      </div>

      {schema && editingTemplate !== undefined && (
        <TemplateDialog
          schema={schema}
          template={editingTemplate}
          pending={templateMutation.isPending}
          onClose={() => setEditingTemplate(undefined)}
          onSave={(input) => templateMutation.mutate(input)}
        />
      )}
      <ConfirmDialog
        open={Boolean(deletingTemplate)}
        onOpenChange={(open) => !open && setDeletingTemplate(null)}
        title={t('Delete configuration template')}
        desc={t(
          'Bound templates cannot be deleted. This removes only the local template.'
        )}
        destructive
        isLoading={deleteMutation.isPending}
        handleConfirm={() =>
          deletingTemplate && deleteMutation.mutate(deletingTemplate.id)
        }
      />
    </section>
  )
}

function DiffTable({ preview }: { preview: ManagedConfigPreview }) {
  const { t } = useTranslation()
  if (!preview.drifted) {
    return (
      <div className='flex items-center gap-2 text-sm text-emerald-700 dark:text-emerald-400'>
        <CheckCircle2 className='size-4' />
        {t('The whitelisted configuration matches the template.')}
      </div>
    )
  }
  return (
    <div className='overflow-x-auto rounded-md border'>
      <table className='w-full min-w-[560px] text-left text-sm'>
        <thead className='bg-muted/50 text-muted-foreground text-xs'>
          <tr>
            <th className='px-3 py-2 font-medium'>{t('Field')}</th>
            <th className='px-3 py-2 font-medium'>{t('Current')}</th>
            <th className='px-3 py-2 font-medium'>{t('Desired')}</th>
          </tr>
        </thead>
        <tbody className='divide-y'>
          {preview.differences.map((difference) => (
            <tr key={difference.key}>
              <td className='px-3 py-2 font-mono text-xs'>{difference.key}</td>
              <td className='max-w-64 px-3 py-2 break-all'>
                {formatValue(difference.current)}
              </td>
              <td className='max-w-64 px-3 py-2 break-all'>
                {formatValue(difference.desired)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function OperationStatus({
  operation,
}: {
  operation: ManagedInstanceOperation
}) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-wrap items-center gap-2 border-t pt-4 text-sm'>
      <span className='text-muted-foreground'>{t('Latest apply')}</span>
      <Badge variant='outline'>{t(operation.status)}</Badge>
      {operation.error_code && (
        <span className='text-destructive font-mono text-xs'>
          {operation.error_code}
        </span>
      )}
      {operation.result?.compensated && (
        <Badge variant='secondary'>{t('Rolled back')}</Badge>
      )}
    </div>
  )
}

function DriftBadge({ status }: { status: string }) {
  const { t } = useTranslation()
  return (
    <Badge
      variant='outline'
      className={cn(
        status === 'in_sync' &&
          'border-emerald-600/20 bg-emerald-600/10 text-emerald-700 dark:text-emerald-400',
        status === 'drifted' &&
          'border-amber-600/20 bg-amber-600/10 text-amber-700 dark:text-amber-400',
        status === 'failed' &&
          'border-red-600/20 bg-red-600/10 text-red-700 dark:text-red-400'
      )}
    >
      {t(status)}
    </Badge>
  )
}

function IconAction({
  label,
  children,
  ...props
}: React.ComponentProps<typeof Button> & { label: string }) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={<Button variant='outline' size='icon' {...props} />}
      >
        {children}
        <span className='sr-only'>{label}</span>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

function TemplateDialog({
  schema,
  template,
  pending,
  onClose,
  onSave,
}: {
  schema: ManagedConfigSchema
  template: ManagedConfigTemplate | null
  pending: boolean
  onClose: () => void
  onSave: (input: ManagedConfigTemplateInput) => void
}) {
  const { t } = useTranslation()
  const [name, setName] = useState(template?.name ?? '')
  const [description, setDescription] = useState(template?.description ?? '')
  const [enabled, setEnabled] = useState(
    () => new Set(Object.keys(template?.values ?? {}))
  )
  const [values, setValues] = useState<Record<string, unknown>>(
    template?.values ?? {}
  )
  const save = () => {
    const selectedValues = Object.fromEntries(
      [...enabled].map((key) => [key, values[key]])
    )
    onSave({
      name: name.trim(),
      description: description.trim(),
      kind: schema.kind,
      schema_version: schema.version,
      values: selectedValues,
    })
  }
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>
            {t(
              template
                ? 'Edit configuration template'
                : 'New configuration template'
            )}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Only selected fields from schema version {{version}} are stored.',
              { version: schema.version }
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='grid gap-4'>
          <div className='grid gap-1.5'>
            <Label htmlFor='template-name'>{t('Name')}</Label>
            <Input
              id='template-name'
              value={name}
              maxLength={128}
              onChange={(event) => setName(event.target.value)}
            />
          </div>
          <div className='grid gap-1.5'>
            <Label htmlFor='template-description'>{t('Description')}</Label>
            <Input
              id='template-description'
              value={description}
              maxLength={512}
              onChange={(event) => setDescription(event.target.value)}
            />
          </div>
          <div className='divide-y rounded-md border'>
            {schema.fields.map((field) => (
              <TemplateField
                key={field.key}
                field={field}
                enabled={enabled.has(field.key)}
                value={values[field.key]}
                onEnabled={(checked) => {
                  const next = new Set(enabled)
                  if (checked) {
                    next.add(field.key)
                    setValues((current) => ({
                      ...current,
                      [field.key]: defaultFieldValue(field),
                    }))
                  } else next.delete(field.key)
                  setEnabled(next)
                }}
                onValue={(value) =>
                  setValues((current) => ({ ...current, [field.key]: value }))
                }
              />
            ))}
          </div>
        </div>
        <DialogFooter>
          <Button variant='outline' onClick={onClose}>
            {t('Cancel')}
          </Button>
          <Button
            disabled={pending || !name.trim() || enabled.size === 0}
            onClick={save}
          >
            {pending ? <LoaderCircle className='animate-spin' /> : <Save />}
            {t('Save template')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function TemplateField({
  field,
  enabled,
  value,
  onEnabled,
  onValue,
}: {
  field: ManagedConfigFieldSchema
  enabled: boolean
  value: unknown
  onEnabled: (checked: boolean) => void
  onValue: (value: unknown) => void
}) {
  let editor: ReactNode
  if (field.type === 'boolean') {
    editor = (
      <Switch
        disabled={!enabled}
        checked={value === true}
        onCheckedChange={onValue}
      />
    )
  } else if (field.enum) {
    editor = (
      <NativeSelect
        className='w-full'
        disabled={!enabled}
        value={String(value ?? field.enum[0])}
        onChange={(event) =>
          onValue(
            field.type === 'integer'
              ? Number(event.target.value)
              : event.target.value
          )
        }
      >
        {field.enum.map((option) => (
          <NativeSelectOption key={String(option)} value={String(option)}>
            {String(option)}
          </NativeSelectOption>
        ))}
      </NativeSelect>
    )
  } else {
    editor = (
      <Input
        disabled={!enabled}
        type={field.type === 'integer' ? 'number' : 'text'}
        value={String(value ?? '')}
        min={field.min}
        max={field.max}
        maxLength={field.max_length}
        onChange={(event) =>
          onValue(
            field.type === 'integer'
              ? Number(event.target.value)
              : event.target.value
          )
        }
      />
    )
  }

  return (
    <div className='grid gap-2 p-3 sm:grid-cols-[minmax(180px,0.7fr)_minmax(0,1fr)] sm:items-center'>
      <Label className='flex min-w-0 items-start gap-2 font-normal'>
        <Checkbox
          checked={enabled}
          onCheckedChange={(checked) => onEnabled(checked === true)}
        />
        <span className='min-w-0'>
          <span className='block font-mono text-xs break-all'>{field.key}</span>
          <span className='text-muted-foreground block text-xs'>
            {field.description}
          </span>
        </span>
      </Label>
      {editor}
    </div>
  )
}

function defaultFieldValue(field: ManagedConfigFieldSchema): unknown {
  if (field.enum?.length) return field.enum[0]
  if (field.type === 'boolean') return false
  if (field.type === 'integer') return field.min ?? 0
  return ''
}

function formatValue(value: unknown): string {
  if (value == null) return '-'
  if (typeof value === 'string') return value || '(empty)'
  return JSON.stringify(value)
}

function createIdempotencyKey(): string {
  return `cfg-${Date.now()}-${crypto.randomUUID()}`
}
