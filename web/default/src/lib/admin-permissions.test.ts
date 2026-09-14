import { expect, test } from 'bun:test'

import { adminHomePath, adminLoginRedirect } from './admin-permissions'

test('a new deny-all administrator lands on own basic and security profile', () => {
  const user = {
    id: 2,
    username: 'new-admin',
    role: 10,
    permissions: {
      admin_permissions: { managed_instance: { view: false, create: false } },
    },
  }
  expect(adminHomePath(user)).toBe('/profile')
  for (const requested of [
    undefined,
    '/',
    '/dashboard',
    '/instances',
    '/users',
    '/403',
  ]) {
    expect(adminLoginRedirect(user, requested)).toBe('/profile')
  }
  expect(adminLoginRedirect(user, '/profile')).toBe('/profile')
})

test('home and login honor available functions without accepting external redirects', () => {
  const user = {
    id: 2,
    username: 'admin',
    role: 10,
    permissions: { admin_permissions: { managed_instance: { view: true } } },
  }
  expect(adminHomePath(user)).toBe('/dashboard')
  expect(adminLoginRedirect(user, '/instances?kind=sub2api')).toBe(
    '/instances?kind=sub2api'
  )
  for (const requested of [
    'https://example.com',
    '//example.com',
    '/\\example.com',
    '/sign-in',
  ]) {
    expect(adminLoginRedirect(user, requested)).toBe('/dashboard')
  }
  expect(adminHomePath(null)).toBe('/sign-in')
  expect(adminHomePath({ id: 1, username: 'root', role: 100 })).toBe(
    '/dashboard'
  )
})
