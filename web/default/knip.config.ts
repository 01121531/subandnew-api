import type { KnipConfig } from 'knip'

const config: KnipConfig = {
  ignore: [
    'src/components/ui/**',
    'src/routeTree.gen.ts',
    'src/env.d.ts',
    'src/tanstack-table.d.ts',
  ],
  ignoreDependencies: ['tailwindcss', 'tw-animate-css'],
}

export default config
