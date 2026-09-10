import { LoaderCircle, Plus } from 'lucide-react'
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
  const [discardImport, setDiscardImport] = useState(false)
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
      setDiscardImport(false)
      setImportOpen(false)
    }
  )
  const closeImport = () => {
    if (importMutation.isPending || discardImport) return
    if (text !== '') {
      setDiscardImport(true)
    } else {
      setImportOpen(false)
    }
  }
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
    <div
      className='grid min-w-0 gap-2'
      role='group'
      aria-label={`${t('supplier.actions')}: ${proxy.name || proxy.host}`}
    >
      <ProxyControls
        proxy={proxy}
        pending={mutation.isPending}
        testing={test.isPending}
        testingCurrent={test.isPending && test.variables === proxy.id}
        pendingAction={
          mutation.isPending && action?.proxy.id === proxy.id
            ? action.type
            : undefined
        }
        onTest={() => {
          if (!test.isPending) test.mutate(proxy.id)
        }}
        onStatus={() => {
          if (!mutation.isPending) setAction({ proxy, type: 'status' })
        }}
        onDelete={() => {
          if (!mutation.isPending) setAction({ proxy, type: 'delete' })
        }}
      />
    </div>
  )
  return (
    <div className='supplier-portal grid min-w-0 gap-4'>
      <div className='supplier-filter-band flex flex-wrap items-center justify-between gap-3'>
        <p className='text-sm font-medium' role='status' aria-atomic='true'>
          {t('supplier.proxies')}: {query.data?.total ?? '--'}
        </p>
        <div className='flex flex-wrap items-center gap-3'>
          <Button onClick={() => setImportOpen(true)}>
            <Plus aria-hidden='true' />
            {t('supplier.import')}
          </Button>
          <Freshness
            data={query.data}
            pending={query.isFetching}
            refresh={query.refresh}
          />
        </div>
      </div>
      <QueryState
        pending={query.isPending}
        error={query.error}
        retry={query.refresh}
        hasData={!!query.data}
      >
        {!query.data?.items.length ? (
          <Empty />
        ) : (
          <>
            <div className='grid gap-3 md:hidden'>
              {query.data.items.map((proxy) => (
                <article
                  key={proxy.id}
                  className='supplier-data-item grid min-w-0 gap-3 rounded-md border p-3'
                >
                  <h3 className='min-w-0 text-sm font-medium [overflow-wrap:anywhere]'>
                    {proxy.name || '--'}
                  </h3>
                  <p className='font-mono text-xs break-all'>
                    {proxy.scheme}://{proxy.host}:{proxy.port}
                  </p>
                  <dl className='grid grid-cols-2 gap-3 text-sm'>
                    <div className='min-w-0'>
                      <dt className='text-muted-foreground text-xs'>
                        {t('supplier.status')}
                      </dt>
                      <dd className='[overflow-wrap:anywhere]'>
                        {t(`supplier.${proxy.status}`, {
                          defaultValue: proxy.status || '--',
                        })}
                      </dd>
                    </div>
                    <div className='min-w-0'>
                      <dt className='text-muted-foreground text-xs'>
                        {t('supplier.ui_ownership')}
                      </dt>
                      <dd className='mt-1 [&_[data-slot=badge]]:h-auto [&_[data-slot=badge]]:max-w-full [&_[data-slot=badge]]:rounded-sm [&_[data-slot=badge]]:[overflow-wrap:anywhere] [&_[data-slot=badge]]:whitespace-normal'>
                        <ProxyOwnership proxy={proxy} />
                      </dd>
                    </div>
                    <div className='min-w-0'>
                      <dt className='text-muted-foreground text-xs'>
                        {t('supplier.health')}
                      </dt>
                      <dd className='[overflow-wrap:anywhere]'>
                        {t(`supplier.${proxy.health_status}`, {
                          defaultValue: proxy.health_status ?? '--',
                        })}
                      </dd>
                    </div>
                    <div className='min-w-0'>
                      <dt className='text-muted-foreground text-xs'>
                        {t('supplier.latency')}
                      </dt>
                      <dd className='[overflow-wrap:anywhere] tabular-nums'>
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
              <Table aria-label={t('supplier.proxies')}>
                <TableHeader>
                  <TableRow>
                    {[
                      'name',
                      'host',
                      'status',
                      'ui_ownership',
                      'health',
                      'latency',
                      'actions',
                    ].map((key) => (
                      <TableHead key={key} scope='col'>
                        {t(`supplier.${key}`)}
                      </TableHead>
                    ))}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {query.data.items.map((proxy) => (
                    <TableRow key={proxy.id}>
                      <TableCell className='max-w-48 min-w-32 break-all whitespace-normal'>
                        <span className='font-medium' title={proxy.name}>
                          {proxy.name || '--'}
                        </span>
                      </TableCell>
                      <TableCell className='max-w-64 min-w-40 font-mono text-xs break-all whitespace-normal'>
                        {proxy.scheme}://{proxy.host}:{proxy.port}
                      </TableCell>
                      <TableCell className='max-w-36 break-all whitespace-normal'>
                        {t(`supplier.${proxy.status}`, {
                          defaultValue: proxy.status || '--',
                        })}
                      </TableCell>
                      <TableCell>
                        <ProxyOwnership proxy={proxy} />
                      </TableCell>
                      <TableCell className='max-w-36 break-all whitespace-normal'>
                        {t(`supplier.${proxy.health_status}`, {
                          defaultValue: proxy.health_status || '--',
                        })}
                      </TableCell>
                      <TableCell className='tabular-nums'>
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
          if (!open) closeImport()
        }}
      >
        <DialogContent
          className='supplier-portal supplier-experience flex max-h-[90dvh] flex-col gap-0 overflow-hidden rounded-lg p-0 sm:max-w-lg'
          aria-describedby={undefined}
          showCloseButton={!importMutation.isPending}
        >
          <div className='supplier-modal-band shrink-0 border-b p-4 pr-12'>
            <DialogTitle className='leading-normal [overflow-wrap:anywhere]'>
              {t('supplier.import')}
            </DialogTitle>
          </div>
          <form
            className='flex min-h-0 flex-1 flex-col'
            onSubmit={(event) => {
              event.preventDefault()
              if (text.trim() && !importMutation.isPending && !discardImport) {
                importMutation.mutate()
              }
            }}
          >
            <div className='min-h-0 flex-1 overflow-y-auto p-4'>
              <Field id='proxy-import' label={t('supplier.proxyText')}>
                <Textarea
                  id='proxy-import'
                  className='min-h-48 font-mono text-xs'
                  value={text}
                  onChange={(event) => setText(event.target.value)}
                  autoComplete='off'
                  spellCheck={false}
                  required
                  disabled={importMutation.isPending}
                />
              </Field>
            </div>
            <div className='supplier-modal-band flex shrink-0 flex-wrap items-center justify-end gap-2 border-t p-4'>
              <Button
                type='button'
                variant='outline'
                disabled={importMutation.isPending}
                onClick={closeImport}
              >
                {t('supplier.cancel')}
              </Button>
              <Button
                type='submit'
                disabled={importMutation.isPending || !text.trim()}
                aria-busy={importMutation.isPending}
              >
                {importMutation.isPending && (
                  <LoaderCircle className='animate-spin' aria-hidden='true' />
                )}
                {t('supplier.import')}
              </Button>
            </div>
          </form>
          <Confirm
            className='supplier-experience'
            open={discardImport}
            title={t('supplier.ui_discardTitle')}
            description={t('supplier.ui_discardDescription')}
            pending={importMutation.isPending}
            destructive
            onClose={() => setDiscardImport(false)}
            onConfirm={() => {
              if (importMutation.isPending) return
              setDiscardImport(false)
              setImportOpen(false)
              setText('')
            }}
          />
        </DialogContent>
      </Dialog>
      <Confirm
        className='supplier-experience'
        open={!!action}
        title={action?.proxy.name ?? ''}
        description={confirmationText}
        pending={mutation.isPending}
        destructive={action?.type === 'delete'}
        onClose={() => setAction(null)}
        onConfirm={() => mutation.mutate()}
      />
    </div>
  )
}
