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
  enabled: boolean
}

export interface AlertDelivery {
  id: number
  recipient: string
  status: string
  attempts: number
  last_error: string
  sent_at: number
}

export interface AlertRecord {
  id: number
  event_type: string
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
  created_at: number
  deliveries: AlertDelivery[]
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

export async function listBillingTemplates() {
  return (
    await api.get<ApiResponse<BillingTemplate[]>>(
      '/api/billing/filter-templates'
    )
  ).data
}

export async function createBillingTemplate(
  input: Pick<BillingTemplate, 'name' | 'description' | 'filters'>
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
  input: Pick<BillingTemplate, 'name' | 'description' | 'filters'>
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
  input: Pick<BillingTemplate, 'name' | 'description' | 'filters'>
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
