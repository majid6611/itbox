export interface ConfigField {
  key: string
  label: string
  type: string
  default: string
  hidden?: boolean
}

export interface ModuleLink {
  name: string
  hostname: string
}

export interface SoftDependency {
  id: string
  integration: string
}

export interface ModuleRoute {
  name?: string
  service: string
  port: number
}

export interface Manifest {
  id: string
  name: string
  description: string
  category: string
  version: string
  available: boolean
  config_schema: ConfigField[]
  soft_dependencies: SoftDependency[]
  routes?: ModuleRoute[]
  internal_panel?: string
  connect_note?: string
  address_kind?: string
  needs_tls?: boolean
}

export type ModuleStatusValue = 'not_installed' | 'installing' | 'running' | 'stopped' | 'error'

export interface ModuleStatus {
  module_id: string
  status: ModuleStatusValue
  config: Record<string, string>
  installed_at?: string
  error_message?: string
  visibility: 'public' | 'private'
  private_port?: number
}

export type UpdateSeverity = 'security' | 'recommended' | 'optional'

export interface ModuleUpdate {
  module_id: string
  name: string
  current_version: string
  latest_version: string
  severity: UpdateSeverity
  changelog: string
  new: boolean
  status?: 'updating' | 'failed'
  message?: string
}

export interface ModulesResponse {
  catalog: Manifest[]
  statuses: ModuleStatus[]
  links: Record<string, ModuleLink[]>
  updates: Record<string, ModuleUpdate>
}

export interface CheckUpdatesResponse {
  updates: ModuleUpdate[]
}

export interface CompanyUser {
  username: string
  email: string
  name: string
  group: string
  disabled: boolean
  last_login?: string
  last_login_ip?: string
  signed_in: boolean
}

export interface UsersResponse {
  available: boolean
  users: CompanyUser[]
}

export interface CompanyGroup {
  name: string
  members: string[]
}

export interface GroupsResponse {
  available: boolean
  groups: CompanyGroup[]
}

export interface CreateUserResponse {
  success: boolean
  password: string
}

export interface ResetPasswordResponse {
  success: boolean
  password: string
}

export interface VpnUser {
  username: string
  name: string
  has_access: boolean
}

export interface VpnDevice {
  name: string
  ip: string
  connected: boolean
  last_seen: string
  os: string
}

export interface VpnUsersResponse {
  available: boolean
  domain_configured: boolean
  users: VpnUser[]
  devices: VpnDevice[]
}

export interface EnableVpnResponse {
  success: boolean
  setup_key: string
}

export type ThemeName = 'slate' | 'stone'

export interface SettingsResponse {
  base_domain: string
  theme: ThemeName
  platform_version: string
}

export interface ThemeResponse {
  theme: ThemeName
}

export interface MeshDevice {
  id: number
  name: string
}

export interface MeshDevicesResponse {
  available: boolean
  devices: MeshDevice[]
}

export interface BackupConfig {
  destination: 'local' | 'aws'
  aws_access_key_id: string
  aws_secret_access_key: string
  aws_bucket: string
  aws_region: string
  schedule: 'off' | 'daily' | 'weekly'
}

export interface BackupRun {
  kind: 'backup' | 'restore' | 'restore-platform'
  started_at: string
  finished_at?: string
  status: 'running' | 'success' | 'error'
  error_message?: string
}

export interface PortalMe {
  username: string
  group: string
}

export interface WikiPageSummary {
  id: number
  path: string
  title: string
}

export interface WikiPage {
  id: number
  path: string
  title: string
  content: string
  can_write: boolean
  updated_at: string
}

export interface WikiRevision {
  id: number
  author: string
  created_at: string
}

export interface WikiAttachment {
  id: number
  filename: string
  size_bytes: number
}

export interface WikiPermissionRule {
  group: string
  access: 'read' | 'write'
}

export interface ChatAttachment {
  id: number
  filename: string
  size_bytes: number
}

export interface ChatMessage {
  id: number
  sender_username: string
  group_name?: string
  recipient_username?: string
  custom_group_id?: number
  content: string
  created_at: string
  edited_at?: string
  deleted_at?: string
  attachment?: ChatAttachment
}

export interface ChatUser {
  username: string
  online: boolean
}

export interface ChatCustomGroup {
  id: number
  name: string
  created_by: string
  members: string[]
}

export type CalendarKind = 'company' | 'personal'

export interface CalendarEvent {
  uid: string
  calendar: CalendarKind
  title: string
  description: string
  start: string
  end: string
  all_day: boolean
  created_by?: string
  attendees?: string[]
  video_call_url?: string
}

export interface DirectoryUser {
  username: string
  name: string
}

export interface CalendarEventsResponse {
  available: boolean
  events: CalendarEvent[]
}

export type CalendarDefaultView = 'month' | 'week' | 'day'

export interface CalendarSettings {
  start_time: string
  end_time: string
  slot_duration_minutes: number
  default_view: CalendarDefaultView
  member_colors: Record<string, string>
}

export interface ChatEvent {
  type: 'message' | 'message_updated' | 'presence' | 'group_invite'
  message?: ChatMessage
  presence?: { username: string; online: boolean }
  group?: { id: number; name: string }
}

export type PhoneState = 'online' | 'in_call' | 'offline' | 'unknown'

export interface PhoneExtension {
  number: string
  name: string
  username: string | null
  password: string
  in_main_group: boolean
  state: PhoneState
}

export interface PhoneSettings {
  trunk_enabled: boolean
  trunk_host: string
  trunk_username: string
  trunk_password_set: boolean
  trunk_caller_id: string
  allow_international: boolean
  trunk_status: 'off' | 'connected' | 'not_connected'
}

export interface PhoneCall {
  from: string
  to: string
  start: string
  talk_seconds: number
  result: 'answered' | 'voicemail' | 'missed' | 'busy' | 'failed'
}

export interface MyPhone {
  number: string
  name: string
  password: string
}

export interface PhoneDirectoryEntry {
  number: string
  name: string
}

export interface PhoneMenuKey {
  digit: string
  label: string
  targets: string[]
}

export interface PhoneMenu {
  enabled: boolean
  recorder: string
  record_code: string
  has_greeting: boolean
  greeting_updated_at?: string
  options: PhoneMenuKey[]
}
