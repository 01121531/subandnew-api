/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.
*/
import { api } from '@/lib/api'

export interface ApiResponse<T> {
  success: boolean
  message: string
  data: T
}

export type BillingFilters = Record<string, string[]>

export interface BillingTemplate {
  id: number
  name: string
  description: string
  system_kind: 'new_api' | 'sub2api' | 'conductor' | ''
  current_version: number
  enabled: boolean
  filters: BillingFilters
  created_at: number
  updated_at: number
}

export interface TemplateImpact {
  template_id: number
  current_version: number
  next_version: number
  rule_count: number
  instance_count: number
  reset_cycle_count: number
  added_fields: string[]
  removed_fields: string[]
  changed_fields: string[]
}

export interface BillingThreshold {
  id?: number
  name: string
  severity: string
  currency: 'USD' | 'CNY'
  amount: string
  reminder_mode: 'once_per_cycle' | 'repeat_interval' | 'repeat_increment'
  repeat_interval_seconds: number
  repeat_increment: string
}

export interface BillingRule {
  id: number
  name: string
  description: string
  template_id: number
  enabled: boolean
  timezone: string
  cycle_type: string
  cycle_config: string
  discount_rate: string
  exchange_mode: string
  manual_exchange_rate: string
  exchange_override: boolean
  schedule_type: string
  schedule_config: string
  recipients: string[]
  failure_threshold: number
  instance_ids: number[]
  thresholds: BillingThreshold[]
  created_at: number
  updated_at: number
}

export type BillingRuleInput = Omit<
  BillingRule,
  'id' | 'created_at' | 'updated_at' | 'cycle_config' | 'schedule_config'
> & {
  cycle_config: Record<string, unknown>
  schedule_config: Record<string, unknown>
}

export interface RuleImpact {
  rule_id: number
  template_id: number
  template_version: number
  instance_count: number
  threshold_count: number
  reset_cycle_count: number
}

export interface ExchangeSetting {
  id: number
  automatic: boolean
  default_mode: string
  manual_rate: string
  update_times: string
  timezone: string
  latest_rate_id: number
  last_attempt_at: number
  last_succeeded_at: number
  last_error_code: string
}

export interface ExchangeRate {
  id: number
  rate: string
  observed_date: string
  source: string
  fallback: boolean
  fetched_at: number
}

export interface SMTPSetting {
  id: number
  host: string
  port: number
  security: string
  username: string
  password_stored: boolean
  from_name: string
  from_address: string
  reply_to: string
  alert_recipients: string
  instance_alert_failure_threshold: number
  enabled: boolean
}

export interface AlertDelivery {
  id: number
  phase?: 'failure' | 'recovery'
  recipient: string
  status: string
  attempts: number
  last_error: string
  next_retry_at: number
  sent_at: number
}

export interface AlertRecord {
  id: number
  event_type: string
  source_type: 'billing' | 'metric' | 'instance'
  source_record_id: number
  rule_id: number
  rule_name: string
  instance_id: number
  instance_name: string
  instance_kind: string
  threshold_name: string
  currency: string
  threshold: string
  usd_total: string
  cny_total: string
  discount_rate: string
  exchange_rate: string
  recipients: string
  error_code: string
  scope_mode: string
  metric_key: string
  conditions: string
  observed_values: string
  created_at: number
  deliveries: AlertDelivery[]
}

export interface MetricAlertCondition {
  id?: number
  metric: string
  operator: 'gt' | 'gte' | 'lt' | 'lte' | 'eq' | 'ne'
  threshold: string
  recovery_threshold: string
}

export interface MetricAlertState {
  id: number
  scope_key: string
  instance_id: number
  active: boolean
  consecutive_violations: number
  consecutive_recoveries: number
  consecutive_failures: number
  failure_notified: boolean
  last_values: string
  last_error_code: string
  last_observed_at: number
}

export interface MetricAlertRule {
  id: number
  name: string
  description: string
  enabled: boolean
  scope_mode: 'per_instance' | 'aggregate'
  match_mode: 'all' | 'any'
  evaluation_interval_seconds: 10 | 30 | 60 | 300
  trigger_count: number
  recovery_count: number
  failure_threshold: number
  reminder_mode: 'once' | 'repeat_interval'
  repeat_interval_seconds: number
  recipients: string[]
  instance_ids: number[]
  conditions: MetricAlertCondition[]
  states: MetricAlertState[]
  next_run_at: number
  last_evaluated_at: number
  created_at: number
  updated_at: number
}

export type MetricAlertRuleInput = Omit<
  MetricAlertRule,
  | 'id'
  | 'states'
  | 'next_run_at'
  | 'last_evaluated_at'
  | 'created_at'
  | 'updated_at'
>

export interface MetricAlertCapability {
  key: string
  label: string
  unit: string
  kinds: string[]
  aggregatable: boolean
}

export interface AlertRecordPage {
  items: AlertRecord[]
  total: number
  page: number
  page_size: number
}

export interface AlertRecordExport {
  id: number
  task_id: string
  status: string
  file_name: string
  file_size: number
  record_count: number
  error_code: string
  started_at: number
  finished_at: number
  expires_at: number
  created_at: number
}

export interface InstanceAlert {
  id: number
  instance_id: number
  instance_name: string
  instance_kind: string
  alert_type: 'availability' | 'credential'
  status: 'open' | 'resolved'
  error_code: string
  occurrences: number
  first_seen_at: number
  last_seen_at: number
  resolved_at: number
  email_status: 'pending' | 'retrying' | 'sent' | 'cancelled'
  email_recipients: string
  email_attempts: number
  email_error: string
  email_sent_at: number
  email_next_retry_at: number
  recovery_email_status: 'pending' | 'retrying' | 'sent' | 'cancelled'
  recovery_email_recipients: string
  recovery_email_attempts: number
  recovery_email_error: string
  recovery_email_sent_at: number
  recovery_email_next_retry_at: number
}

export interface InstanceAlertPage {
  items: InstanceAlert[]
  total: number
  page: number
  page_size: number
}

export interface InstanceAlertRule {
  id: number
  name: string
  description: string
  enabled: boolean
  alert_types: Array<'availability' | 'credential'>
  check_interval_seconds: number
  failure_threshold: number
  effective_failure_threshold: number
  recipients: string[]
  effective_recipients: string[]
  instance_ids: number[]
  created_at: number
  updated_at: number
}

export type InstanceAlertRuleInput = Pick<
  InstanceAlertRule,
  | 'name'
  | 'description'
  | 'enabled'
  | 'alert_types'
  | 'check_interval_seconds'
  | 'failure_threshold'
  | 'recipients'
  | 'instance_ids'
>

export async function listBillingTemplates() {
  return (
    await api.get<ApiResponse<BillingTemplate[]>>(
      '/api/billing/filter-templates'
    )
  ).data
}

export async function createBillingTemplate(
  input: Pick<
    BillingTemplate,
    'name' | 'description' | 'system_kind' | 'filters'
  >
) {
  return (
    await api.post<ApiResponse<BillingTemplate>>(
      '/api/billing/filter-templates',
      input
    )
  ).data
}

export async function updateBillingTemplate(
  id: number,
  input: Pick<
    BillingTemplate,
    'name' | 'description' | 'system_kind' | 'filters'
  >
) {
  return (
    await api.put<ApiResponse<BillingTemplate>>(
      `/api/billing/filter-templates/${id}`,
      input
    )
  ).data
}

export async function previewBillingTemplate(
  id: number,
  input: Pick<
    BillingTemplate,
    'name' | 'description' | 'system_kind' | 'filters'
  >
) {
  return (
    await api.post<ApiResponse<TemplateImpact>>(
      `/api/billing/filter-templates/${id}/preview`,
      input
    )
  ).data
}

export async function deleteBillingTemplate(id: number) {
  return (await api.delete(`/api/billing/filter-templates/${id}`)).data
}

export async function listBillingRules() {
  return (await api.get<ApiResponse<BillingRule[]>>('/api/billing/alert-rules'))
    .data
}

export async function createBillingRule(input: BillingRuleInput) {
  return (
    await api.post<ApiResponse<BillingRule>>('/api/billing/alert-rules', input)
  ).data
}

export async function updateBillingRule(id: number, input: BillingRuleInput) {
  return (
    await api.put<ApiResponse<BillingRule>>(
      `/api/billing/alert-rules/${id}`,
      input
    )
  ).data
}

export async function previewBillingRule(
  id: number | null,
  input: BillingRuleInput
) {
  const path = id
    ? `/api/billing/alert-rules/${id}/preview`
    : '/api/billing/alert-rules/preview'
  return (await api.post<ApiResponse<RuleImpact>>(path, input)).data
}

export async function deleteBillingRule(id: number) {
  return (await api.delete(`/api/billing/alert-rules/${id}`)).data
}

export async function evaluateBillingRule(id: number, instanceId: number) {
  return (
    await api.post(`/api/billing/alert-rules/${id}/evaluate`, {
      instance_id: instanceId,
    })
  ).data
}

export async function listMetricAlertRules() {
  return (
    await api.get<ApiResponse<MetricAlertRule[]>>('/api/metric-alert-rules')
  ).data
}

export async function createMetricAlertRule(input: MetricAlertRuleInput) {
  return (
    await api.post<ApiResponse<MetricAlertRule>>(
      '/api/metric-alert-rules',
      input
    )
  ).data
}

export async function updateMetricAlertRule(
  id: number,
  input: MetricAlertRuleInput
) {
  return (
    await api.put<ApiResponse<MetricAlertRule>>(
      `/api/metric-alert-rules/${id}`,
      input
    )
  ).data
}

export async function deleteMetricAlertRule(id: number) {
  return (await api.delete(`/api/metric-alert-rules/${id}`)).data
}

export async function evaluateMetricAlertRule(id: number) {
  return (await api.post(`/api/metric-alert-rules/${id}/evaluate`)).data
}

export async function listMetricAlertCapabilities(
  instanceIds: number[],
  scopeMode: 'per_instance' | 'aggregate'
) {
  const params = new URLSearchParams({
    instance_ids: instanceIds.join(','),
    scope_mode: scopeMode,
  })
  return (
    await api.get<ApiResponse<MetricAlertCapability[]>>(
      `/api/alert-metrics/capabilities?${params}`
    )
  ).data
}

export async function getExchangeSettings() {
  return (
    await api.get<ApiResponse<ExchangeSetting>>(
      '/api/billing/exchange-settings'
    )
  ).data
}

export async function updateExchangeSettings(input: {
  automatic: boolean
  default_mode: string
  manual_rate: string
  update_times: string[]
  timezone: string
}) {
  return (
    await api.put<ApiResponse<ExchangeSetting>>(
      '/api/billing/exchange-settings',
      input
    )
  ).data
}

export async function listExchangeRates() {
  return (
    await api.get<ApiResponse<ExchangeRate[]>>(
      '/api/billing/exchange-rates?limit=20'
    )
  ).data
}

export async function refreshExchangeRate() {
  return (
    await api.post<ApiResponse<ExchangeRate>>(
      '/api/billing/exchange-rates/refresh'
    )
  ).data
}

export async function getSMTPSettings() {
  return (await api.get<ApiResponse<SMTPSetting>>('/api/billing/smtp-settings'))
    .data
}

export async function updateSMTPSettings(
  input: SMTPSetting & { password: string }
) {
  return (
    await api.put<ApiResponse<SMTPSetting>>('/api/billing/smtp-settings', input)
  ).data
}

export async function testSMTPSettings(recipient: string) {
  return (await api.post('/api/billing/smtp-settings/test', { recipient })).data
}

export async function listAlertRecords(params: URLSearchParams) {
  return (
    await api.get<ApiResponse<AlertRecordPage>>(
      `/api/billing/alert-records?${params}`
    )
  ).data
}

export async function listInstanceAlerts(params: URLSearchParams) {
  return (
    await api.get<ApiResponse<InstanceAlertPage>>(
      `/api/billing/instance-alerts?${params}`
    )
  ).data
}

export async function listInstanceAlertRules() {
  return (
    await api.get<ApiResponse<InstanceAlertRule[]>>('/api/instance-alert-rules')
  ).data
}

export async function createInstanceAlertRule(input: InstanceAlertRuleInput) {
  return (
    await api.post<ApiResponse<InstanceAlertRule>>(
      '/api/instance-alert-rules',
      input
    )
  ).data
}

export async function updateInstanceAlertRule(
  id: number,
  input: InstanceAlertRuleInput
) {
  return (
    await api.put<ApiResponse<InstanceAlertRule>>(
      `/api/instance-alert-rules/${id}`,
      input
    )
  ).data
}

export async function deleteInstanceAlertRule(id: number) {
  return (await api.delete(`/api/instance-alert-rules/${id}`)).data
}

export async function getAlertRecord(id: number) {
  return (
    await api.get<ApiResponse<AlertRecord>>(`/api/billing/alert-records/${id}`)
  ).data
}

export async function createAlertRecordExport(params: URLSearchParams) {
  return (
    await api.post<ApiResponse<AlertRecordExport>>(
      `/api/billing/alert-records/exports?${params}`
    )
  ).data
}

export async function listAlertRecordExports() {
  return (
    await api.get<ApiResponse<AlertRecordExport[]>>(
      '/api/billing/alert-record-exports',
      { disableDuplicate: true }
    )
  ).data
}

export async function downloadAlertRecordExport(taskId: string) {
  const response = await api.get(
    `/api/billing/alert-record-exports/${encodeURIComponent(taskId)}/download`,
    { responseType: 'blob', disableDuplicate: true }
  )
  const disposition = String(response.headers['content-disposition'] ?? '')
  const filename =
    disposition.match(/filename="?([^";]+)"?/i)?.[1] ??
    `billing-alerts-${taskId}.csv`
  const objectUrl = URL.createObjectURL(response.data)
  const anchor = document.createElement('a')
  anchor.href = objectUrl
  anchor.download = filename
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(objectUrl)
}
