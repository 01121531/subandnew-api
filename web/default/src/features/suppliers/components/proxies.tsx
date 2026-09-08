import { Plus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'

import { usePortalQuery, useSupplierMutation } from '../hooks/use-portal-query'
import { proxyEnabled } from '../lib/display'
import { portalApi } from '../portal-api'
import type { Proxy } from '../types'
import {
  Confirm,
  Empty,
  Field,
  Freshness,
  Pagination,
  QueryState,
} from './common'
import { ProxyControls, ProxyOwnership } from './proxy-controls'

export function Proxies(props: { bindingId: number; csrf: string }) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const query = usePortalQuery(
    ['proxies', props.bindingId, page],
    (signal, refresh) =>
      portalApi.proxies(props.bindingId, signal, refresh, page)
  )
  const [importOpen, setImportOpen] = useState(false)
  const [text, setText] = useState('')
  const [action, setAction] = useState<{
    proxy: Proxy
    type: 'delete' | 'status'
  } | null>(null)
  const importMutation = useSupplierMutation(
    () => portalApi.importProxies(props.csrf, props.bindingId, text),
    (data) => {
      toast.success(
        t('supplier.importResult', {
          imported: data.imported,
          failed: data.failed,
        })
      )
      setText('')
      setImportOpen(false)
    }
  )
  const test = useSupplierMutation(
    (id: string) => portalApi.testProxy(props.csrf, props.bindingId, id),
    (data) => {
      if (data.ok) toast.success(t('supplier.testOk'))
      else toast.error(t('supplier.testFailed'))
    }
  )
  const mutation = useSupplierMutation(
    async () => {
      if (!action) return
      if (action.type === 'delete') {
        return portalApi.deleteProxy(
          props.csrf,
          props.bindingId,
          action.proxy.id
        )
      }
      if (action.proxy.can_update_status !== true) return
      return portalApi.setProxy(
        props.csrf,
        props.bindingId,
        action.proxy.id,
        proxyEnabled(action.proxy.status) ? 'disabled' : 'enabled'
      )
    },
    () => {
      setAction(null)
      toast.success(t('supplier.completed'))
    }
  )
  let confirmationText = t('supplier.proxyStatusConfirm')
  if (action?.type === 'delete') {
    confirmationText =
      action.proxy.is_owner === false
        ? t('supplier.proxyUnbindConfirm')
        : t('supplier.proxyDeleteConfirm')
  }
  const controls = (proxy: Proxy) => (
    <ProxyControls
      proxy={proxy}
      pending={mutation.isPending}
      testing={test.isPending}
      onTest={() => test.mutate(proxy.id)}
      onStatus={() => setAction({ proxy, type: 'status' })}
      onDelete={() => setAction({ proxy, type: 'delete' })}
    />
  )
  return (
    <div className='grid min-w-0 gap-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        <Button onClick={() => setImportOpen(true)}>
          <Plus />
          {t('supplier.import')}
        </Button>
        <Freshness
          data={query.data}
          pending={query.isFetching}
          refresh={query.refresh}
        />
      </div>
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={query.refresh}
      >
        {!query.data?.items.length ? (
          <Empty />
        ) : (
          <>
            <div className='grid gap-3 md:hidden'>
              {query.data.items.map((proxy) => (
                <article
                  key={proxy.id}
                  className='grid min-w-0 gap-3 rounded-md border p-3'
                >
                  <div className='flex flex-wrap items-center gap-2'>
                    <span className='min-w-0 font-medium break-words'>
                      {proxy.name}
                    </span>
                    <ProxyOwnership proxy={proxy} />
                  </div>
                  <p className='font-mono text-xs break-all'>
                    {proxy.scheme}://{proxy.host}:{proxy.port}
                  </p>
                  <dl className='grid grid-cols-2 gap-3 text-sm'>
                    <div>
                      <dt className='text-muted-foreground text-xs'>
                        {t('supplier.health')}
                      </dt>
                      <dd>
                        {t(`supplier.${proxy.health_status}`, {
                          defaultValue: proxy.health_status ?? '--',
                        })}
                      </dd>
                    </div>
                    <div>
                      <dt className='text-muted-foreground text-xs'>
                        {t('supplier.latency')}
                      </dt>
                      <dd>
                        {proxy.latency_ms == null
                          ? '--'
                          : `${proxy.latency_ms} ms`}
                      </dd>
                    </div>
                  </dl>
                  {controls(proxy)}
                </article>
              ))}
            </div>
            <div className='hidden min-w-0 md:block'>
              <Table>
                <TableHeader>
                  <TableRow>
                    {[
                      'name',
                      'host',
                      'status',
                      'health',
                      'latency',
                      'actions',
                    ].map((key) => (
                      <TableHead key={key}>{t(`supplier.${key}`)}</TableHead>
                    ))}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {query.data.items.map((proxy) => (
                    <TableRow key={proxy.id}>
                      <TableCell>
                        <div className='flex items-center gap-2'>
                          <span
                            className='max-w-40 truncate'
                            title={proxy.name}
                          >
                            {proxy.name}
                          </span>
                          <ProxyOwnership proxy={proxy} />
                        </div>
                      </TableCell>
                      <TableCell className='font-mono text-xs'>
                        {proxy.scheme}://{proxy.host}:{proxy.port}
                      </TableCell>
                      <TableCell>
                        {t(`supplier.${proxy.status}`, {
                          defaultValue: proxy.status,
                        })}
                      </TableCell>
                      <TableCell>
                        {t(`supplier.${proxy.health_status}`, {
                          defaultValue: proxy.health_status,
                        })}
                      </TableCell>
                      <TableCell>
                        {proxy.latency_ms == null
                          ? '--'
                          : `${proxy.latency_ms} ms`}
                      </TableCell>
                      <TableCell>{controls(proxy)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </>
        )}
        <Pagination
          page={page}
          size={20}
          total={query.data?.total ?? 0}
          pending={query.isFetching}
          onPage={setPage}
        />
      </QueryState>
      <Dialog
        open={importOpen}
        onOpenChange={(open) => {
          if (!importMutation.isPending) {
            setImportOpen(open)
            if (!open) setText('')
          }
        }}
      >
        <DialogContent className='sm:max-w-lg'>
          <DialogTitle>{t('supplier.import')}</DialogTitle>
          <form
            className='grid gap-4'
            onSubmit={(event) => {
              event.preventDefault()
              if (text.trim()) importMutation.mutate()
            }}
          >
            <Field id='proxy-import' label={t('supplier.proxyText')}>
              <Textarea
                id='proxy-import'
                className='min-h-48 font-mono text-xs'
                value={text}
                onChange={(event) => setText(event.target.value)}
                autoComplete='off'
                spellCheck={false}
                required
              />
            </Field>
            <Button
              type='submit'
              disabled={importMutation.isPending || !text.trim()}
            >
              {t('supplier.import')}
            </Button>
          </form>
        </DialogContent>
      </Dialog>
      <Confirm
        open={!!action}
        title={action?.proxy.name ?? ''}
        description={confirmationText}
        pending={mutation.isPending}
        onClose={() => setAction(null)}
        onConfirm={() => mutation.mutate()}
      />
    </div>
  )
}
