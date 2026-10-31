// Picks a resource path: the application, the tenant, its knowledge base
// folders, or a typed path such as tenant:1/volume:42 (a Volume's Share
// dialog names its path).

import { Autocomplete, TextField } from '@mui/material'

function resourceOptions(tenant: string, app: boolean): { path: string; label: string }[] {
  return [
    ...(app ? [{ path: 'app', label: 'The application' }] : []),
    { path: tenant, label: 'This tenant' },
    { path: `${tenant}/kb`, label: 'Knowledge base folders outside Volumes' },
  ]
}

function ResourcePicker({ value, onChange, tenant, app, label = 'Resource' }: { value: string; onChange: (path: string) => void; tenant: string; app: boolean; label?: string }) {
  const options = resourceOptions(tenant, app)
  return (
    <Autocomplete
      freeSolo
      size="small"
      options={options.map((o) => o.path)}
      getOptionLabel={(p) => {
        const o = options.find((x) => x.path === p)
        return o ? `${o.label} (${o.path})` : p
      }}
      value={value}
      onInputChange={(_e, v) => onChange(v.replace(/^.*\((.*)\)$/, '$1'))}
      onChange={(_e, v) => onChange(v ?? '')}
      sx={{ minWidth: 260 }}
      renderInput={(params) => <TextField {...params} label={label} helperText="Or type a path, e.g. tenant:1/volume:42" />}
    />
  )
}

export default ResourcePicker
