import { expect, test } from 'bun:test'

import type { Account } from '../types'
import { statusTargets } from './change-status'

const account = (status: string) =>
  ({ status, assignment_id: 1, archived_at: 0 }) as Account
test('status targets intersect without approving unsubmitted tasks', () => {
  expect(statusTargets([account('pending')])).toEqual(['rejected'])
  expect(statusTargets([account('submitted')])).toEqual([
    'approved',
    'rejected',
  ])
  expect(statusTargets([account('approved')])).toEqual(['pending', 'rejected'])
  expect(statusTargets([account('approved'), account('submitted')])).toEqual([
    'rejected',
  ])
  expect(statusTargets([account('pending'), account('rejected')])).toEqual([])
  for (const state of ['unassigned', 'issue_pending']) {
    expect(statusTargets([account(state)])).toEqual([])
  }
  expect(statusTargets([])).toEqual([])
  expect(statusTargets([{ ...account('pending'), archived_at: 1 }])).toEqual([])
  expect(statusTargets([{ ...account('pending'), assignment_id: 0 }])).toEqual(
    []
  )
})
