import { Breadcrumbs, Link, Typography } from '@mui/material'

// Breadcrumbs from rootLabel down through folders; onNavigate receives the
// number of folders to keep
function FolderBreadcrumbs({
  rootLabel,
  folders,
  onNavigate,
}: {
  rootLabel: string
  folders: readonly string[]
  onNavigate: (depth: number) => void
}) {
  return (
    <Breadcrumbs sx={{ flexGrow: 1, minWidth: 0 }}>
      {[rootLabel, ...folders].map((name, index) =>
        index === folders.length ? (
          <Typography key={index} color="text.primary">
            {name}
          </Typography>
        ) : (
          <Link
            key={index}
            component="button"
            underline="hover"
            color="inherit"
            onClick={() => onNavigate(index)}
          >
            {name}
          </Link>
        ),
      )}
    </Breadcrumbs>
  )
}

export default FolderBreadcrumbs
