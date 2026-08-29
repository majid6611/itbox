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

export interface Manifest {
  id: string
  name: string
  description: string
  category: string
  version: string
  available: boolean
  config_schema: ConfigField[]
  soft_dependencies: SoftDependency[]
  internal_panel?: string
}

export type ModuleStatusValue = 'not_installed' | 'installing' | 'running' | 'stopped' | 'error'

export interface ModuleStatus {
  module_id: string
  status: ModuleStatusValue
  config: Record<string, string>
  installed_at?: string
  error_message?: string
}

export interface ModulesResponse {
  catalog: Manifest[]
  statuses: ModuleStatus[]
  links: Record<string, ModuleLink[]>
}

export interface CompanyUser {
  username: string
  email: string
  name: string
  group: string
  disabled: boolean
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

export interface VpnUsersResponse {
  available: boolean
  domain_configured: boolean
  users: VpnUser[]
}

export interface EnableVpnResponse {
  success: boolean
  setup_key: string
}

export interface SettingsResponse {
  base_domain: string
}
