// The Material icons server-described trees may name, by name
import AddIcon from '@mui/icons-material/Add'
import CheckIcon from '@mui/icons-material/Check'
import CloseIcon from '@mui/icons-material/Close'
import DeleteIcon from '@mui/icons-material/Delete'
import DescriptionIcon from '@mui/icons-material/Description'
import EditIcon from '@mui/icons-material/Edit'
import ErrorIcon from '@mui/icons-material/Error'
import FolderIcon from '@mui/icons-material/Folder'
import HelpIcon from '@mui/icons-material/Help'
import HomeIcon from '@mui/icons-material/Home'
import InfoIcon from '@mui/icons-material/Info'
import InboxIcon from '@mui/icons-material/Inbox'
import MailIcon from '@mui/icons-material/Mail'
import MoreVertIcon from '@mui/icons-material/MoreVert'
import PersonIcon from '@mui/icons-material/Person'
import PlayArrowIcon from '@mui/icons-material/PlayArrow'
import RemoveIcon from '@mui/icons-material/Remove'
import SearchIcon from '@mui/icons-material/Search'
import SendIcon from '@mui/icons-material/Send'
import SettingsIcon from '@mui/icons-material/Settings'
import StarIcon from '@mui/icons-material/Star'
import WarningIcon from '@mui/icons-material/Warning'
import WavingHandIcon from '@mui/icons-material/WavingHand'
import ChecklistIcon from '@mui/icons-material/Checklist'
import AccountTreeIcon from '@mui/icons-material/AccountTree'
import AppsIcon from '@mui/icons-material/Apps'
import ArrowUpwardIcon from '@mui/icons-material/ArrowUpward'
import ArrowDownwardIcon from '@mui/icons-material/ArrowDownward'
import ArrowBackIcon from '@mui/icons-material/ArrowBack'
import ArrowForwardIcon from '@mui/icons-material/ArrowForward'
import ListIcon from '@mui/icons-material/List'
import TaskIcon from '@mui/icons-material/Task'
import TableRowsIcon from '@mui/icons-material/TableRows'
import OpenInNewIcon from '@mui/icons-material/OpenInNew'
import SwapHorizIcon from '@mui/icons-material/SwapHoriz'
import TaskAltIcon from '@mui/icons-material/TaskAlt'
import type { SvgIconProps } from '@mui/material'
import { createElement, type ComponentType } from 'react'

export const icons: Record<string, ComponentType<SvgIconProps>> = {
  add: AddIcon,
  check: CheckIcon,
  close: CloseIcon,
  delete: DeleteIcon,
  description: DescriptionIcon,
  edit: EditIcon,
  error: ErrorIcon,
  folder: FolderIcon,
  help: HelpIcon,
  home: HomeIcon,
  info: InfoIcon,
  inbox: InboxIcon,
  mail: MailIcon,
  more_vert: MoreVertIcon,
  person: PersonIcon,
  play_arrow: PlayArrowIcon,
  remove: RemoveIcon,
  search: SearchIcon,
  send: SendIcon,
  settings: SettingsIcon,
  star: StarIcon,
  warning: WarningIcon,
  waving_hand: WavingHandIcon,
  checklist: ChecklistIcon,
  account_tree: AccountTreeIcon,
  apps: AppsIcon,
  arrow_upward: ArrowUpwardIcon,
  arrow_downward: ArrowDownwardIcon,
  // An app's frame and navigation (forge 05d)
  arrow_back: ArrowBackIcon,
  arrow_forward: ArrowForwardIcon,
  list: ListIcon,
  open_in_new: OpenInNewIcon,
  swap_horiz: SwapHorizIcon,
  task_alt: TaskAltIcon,
  task: TaskIcon,
  table_rows: TableRowsIcon,
}

// An icon by name, as a component for places that take one (the Editors view)
export function iconComponent(name?: string): ComponentType<{ fontSize?: 'small' | 'medium' }> {
  const Svg = (name && icons[name]) || AppsIcon
  return ({ fontSize }) => createElement(Svg, { fontSize })
}
