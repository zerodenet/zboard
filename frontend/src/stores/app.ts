import { defineStore } from 'pinia'
import { clearAuthToken, fetchPublicSystemConfigs, fetchSystemStatus, getAuthToken, getSetupStatus, setAuthToken, me as fetchMe, type Announcement, type MaintenanceState, type SetupStatus, type SystemConfig } from '../api/client'
import { applySiteMetadata, buildSiteProfile } from '../utils/siteProfile'
import { DEFAULT_SYSTEM_TIME_ZONE, setDisplayTimeZone } from '../utils/timeZone'

export const useAppStore = defineStore('app', {
  state: () => ({
    token: getAuthToken(),
    installation: null as SetupStatus | null,
    setupChecked: false,
	publicConfigs: [] as SystemConfig[],
	maintenance: { enabled: false, title: '系统维护中', message: '系统正在维护，请稍后再试。', migration_in_progress: false, migration_cutover_pending: false } as MaintenanceState,
	announcements: [] as Announcement[],
	systemStatusLoadedAt: 0,
    systemTimeZone: DEFAULT_SYSTEM_TIME_ZONE,
    user: {
      id: 0,
      email: '',
      isAdmin: false,
    }
  }),
  getters: {
    isAuthenticated: (state) => Boolean(state.token),
    isAdmin: (state) => Boolean(state.user?.isAdmin),
    isInstalled: (state) => Boolean(state.installation?.installed),
    siteName: (state) => buildSiteProfile(state.publicConfigs, state.installation?.site_name || '').name,
    siteProfile: (state) => buildSiteProfile(state.publicConfigs, state.installation?.site_name || ''),
  },
  actions: {
	async loadSystemStatus(force = false) {
		if (!force && Date.now() - this.systemStatusLoadedAt < 5000) {
			return { maintenance: this.maintenance, announcements: this.announcements, as_of: new Date(this.systemStatusLoadedAt).toISOString() }
		}
		const status = await fetchSystemStatus()
		this.maintenance = status.maintenance
		this.announcements = status.announcements || []
		this.systemStatusLoadedAt = Date.now()
		return status
	},
    setSystemTimeZone(value: unknown) {
      this.systemTimeZone = setDisplayTimeZone(value)
      return this.systemTimeZone
    },
    async loadPublicConfigs() {
      try {
        this.publicConfigs = await fetchPublicSystemConfigs()
        const timezone = this.publicConfigs.find(item => item.config_key === 'system_timezone')
        this.setSystemTimeZone(timezone?.value)
        applySiteMetadata(buildSiteProfile(this.publicConfigs, this.installation?.site_name || ''))
        return this.publicConfigs
      } catch (_) {
        this.publicConfigs = []
        this.setSystemTimeZone(DEFAULT_SYSTEM_TIME_ZONE)
        applySiteMetadata(buildSiteProfile([], this.installation?.site_name || ''))
        return this.publicConfigs
      }
    },
    async loadSystemTimeZone() {
      if (!this.publicConfigs.length) await this.loadPublicConfigs()
      const config = this.publicConfigs.find(item => item.config_key === 'system_timezone')
      return this.setSystemTimeZone(config?.value)
    },
    async loadSetupStatus(force = false) {
      if (this.setupChecked && !force) {
        return this.installation
      }
      this.installation = await getSetupStatus()
      this.setupChecked = true
      if (this.installation?.installed) await this.loadPublicConfigs()
      else {
        this.publicConfigs = []
        this.setSystemTimeZone(DEFAULT_SYSTEM_TIME_ZONE)
      }
      return this.installation
    },
    completeSetup(result: any) {
      this.installation = {
        installed: true,
        site_name: result.site_name || 'zboard',
        version: ''
      }
      this.setupChecked = true
      this.publicConfigs = []
      this.setSystemTimeZone(DEFAULT_SYSTEM_TIME_ZONE)
      applySiteMetadata(buildSiteProfile([], this.installation.site_name))
      if (result.auth?.token) {
        this.setToken(result.auth.token)
      }
      if (result.user) {
        this.setUser(result.user)
      }
    },
    setToken(token: string) {
      this.token = token
      setAuthToken(token)
    },
    setUser(user: any) {
      this.user = {
        id: user.id,
        email: user.email,
        isAdmin: Boolean(user.is_admin ?? user.isAdmin)
      }
    },
    async loadMe() {
      if (!this.token) {
        return
      }
      try {
        const user = await fetchMe()
        if (user) {
          this.setUser(user)
        } else {
          this.clear()
        }
      } catch (_) {
        this.clear()
      }
    },
    clear() {
      this.token = ''
      this.user = { id: 0, email: '', isAdmin: false }
      clearAuthToken()
    }
  }
})
