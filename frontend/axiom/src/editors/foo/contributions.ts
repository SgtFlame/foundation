import DescriptionIcon from '@mui/icons-material/Description'
import { lazy } from 'react'
import { FooObject } from './FooObject.ts'
import { extensions } from '../../workbench/extensions/index.ts'
import { commands, editors } from '../../workbench/points.ts'

extensions.contribute(editors, {
  id: 'foo',
  source: 'foo',
  name: 'Foo',
  component: lazy(() => import('./FooEditor.tsx')),
})

extensions.contribute(commands, {
  id: 'open-foo',
  source: 'foo',
  order: 200,
  title: 'Foo',
  icon: DescriptionIcon,
  section: 'One',
  run: ({ registry }) => registry.edit(new FooObject('foo-1')),
})
