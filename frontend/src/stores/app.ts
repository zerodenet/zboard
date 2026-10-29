import { defineStore } from 'pinia'
import { clearAuthToken, fetchPublicSystemConfigs, fetchSystemStatus, getAuthToken, getSetupStatus, login, markAnnouncementRead as markAnnouncementReadRequest, setAuthToken, me as fetchMe, type Announcement, type MaintenanceState, type SetupStatus, type SystemConfig } from '../api/client'
import { AUTH_SESSION_RESTORED_EVENT, resetAuthSessionExpired } from '../utils/authSession'
import { clearTrackedTasks } from '../utils/taskTracker'
import { fetchIdentityPasswordStatus } from '../api/identities'
import { clearAuthReturnPath } from '../utils/authReturnPath'
import { approveReauthenticationDraftPage, clearReauthenticationDrafts, confirmReauthenticationDraftIdentity, invalidateReauthenticationDraftIdentity, pendingReauthenticationDraftPage } from '../utils/reauthenticationDrafts'
import { fetchNavigation } from '../api/menus'
import { applySiteMetadata, buildSiteProfile } from '../utils/siteProfile'
import { DEFAULT_SYSTEM_TIME_ZONE, setDisplayTimeZone } from '../utils/timeZone'

export const useAppStore = defineStore('app', {
  state: () => ({
    token: getAuthToken(),
    reauthRequired: false,
    sessionEpoch: 0,
    passwordLoginAvailable: null as boolean | null,
    installation: null as SetupStatus | null,
    setupChecked: false,
	publicConfigs: [] as SystemConfig[],
	maintenance: { enabled: false, title: '系统维护中', message: '系统正在维护，请稍后再试。', migration_in_progress: false, migration_cutover_pending: false } as MaintenanceState,
	announcements: [] as Announcement[],
	announcementUnreadCount: 0,
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
			return { maintenance: this.maintenance, announcements: this.announcements, announcement_unread_count: this.announcementUnreadCount, as_of: new Date(this.systemStatusLoadedAt).toISOString() }
		}
		const token = this.token
		const epoch = this.sessionEpoch
		const status = await fetchSystemStatus()
		if (token !== this.token || epoch !== this.sessionEpoch) return status
		this.maintenance = status.maintenance
		this.announcements = status.announcements || []
		this.announcementUnreadCount = Number(status.announcement_unread_count || 0)
		this.systemStatusLoadedAt = Date.now()
		return status
	},
	async markAnnouncementRead(item: Announcement) {
		if (!this.token) return
		const token = this.token
		await markAnnouncementReadRequest(item.id, item.revision)
		if (token !== this.token) return
		this.announcements = this.announcements.map(candidate => candidate.id === item.id ? { ...candidate, read: true, read_at: new Date().toISOString() } : candidate)
		this.announcementUnreadCount = Math.max(0, this.announcementUnreadCount - 1)
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
      if (token !== this.token) invalidateReauthenticationDraftIdentity()
      this.token = token
      setAuthToken(token)
      void this.loadIdentityPasswordStatus()
    },
    async loadIdentityPasswordStatus() {
      const token = this.token
      if (!token || this.reauthRequired) return
      try {
        const status = await fetchIdentityPasswordStatus()
        if (token === this.token && typeof status.password_set === 'boolean') this.passwordLoginAvailable = status.password_set
      } catch { /* Unknown availability does not imply that password login exists. */ }
    },
    setUser(user: any) {
      const isAdmin = Boolean(user.is_admin ?? user.isAdmin)
      if (this.user.id && (this.user.id !== user.id || this.user.isAdmin !== isAdmin)) {
        clearReauthenticationDrafts()
        this.sessionEpoch++
        clearTrackedTasks()
        this.announcements = []
        this.announcementUnreadCount = 0
        this.systemStatusLoadedAt = 0
        this.passwordLoginAvailable = null
      }
      this.user = {
        id: user.id,
        email: user.email,
        isAdmin
      }
    },
    suspendSession() {
      invalidateReauthenticationDraftIdentity()
      this.reauthRequired = true
      this.token = ''
      clearAuthToken()
    },
    async reauthenticate(email: string, password: string) {
      const result = await login(email, password)
      await this.acceptAuthenticatedSession(result.auth.token)
      this.reauthRequired = false
      void this.loadIdentityPasswordStatus()
      clearAuthReturnPath()
      void this.loadSystemStatus(true).catch(() => undefined)
      window.dispatchEvent(new Event(AUTH_SESSION_RESTORED_EVENT))
    },
    async acceptAuthenticatedSession(token: string) {
      invalidateReauthenticationDraftIdentity()
      if (typeof token !== 'string' || !token) throw new Error('无法确认登录会话，请重新登录。')
      // The main identity endpoint, rather than the form or login response,
      // determines which account and permissions can resume this page.
      setAuthToken(token, false)
      let user
      try {
        user = await fetchMe()
        if (!Number.isSafeInteger(user?.id) || user.id <= 0) throw new Error('无法确认登录身份，请重新登录。')
        if (getAuthToken() !== token) throw new Error('登录状态已变化，请重新认证。')
      } catch (cause) {
        if (getAuthToken() === token) clearAuthToken()
        throw cause
      }
      this.setUser(user)
      this.setToken(token)
      this.reauthRequired = false
      confirmReauthenticationDraftIdentity(this.user.id, this.isAdmin)
      const recoveryPage = pendingReauthenticationDraftPage(this.user.id)
      if (recoveryPage) {
        try {
          const access = await fetchNavigation(recoveryPage.startsWith('/admin') ? 'admin' : 'account', undefined, recoveryPage)
          if (getAuthToken() === token) approveReauthenticationDraftPage(this.user.id, recoveryPage, access.page_available === true)
        } catch (cause: any) {
          if (getAuthToken() === token && cause?.response?.status === 403) clearReauthenticationDrafts()
          // A failed permission read is not failed authentication. No draft
          // payload has been read; the mounted module can retry its read later.
        }
      }
      if (getAuthToken() !== token) throw new Error('登录状态已变化，请重新认证。')
      return user
    },
    async loadMe() {
      if (!this.token) {
        return
      }
      const token = this.token
      // HTTP authentication failures are handled by the shared API boundary.
      // A temporary network failure must not log out the user.
      const user = await fetchMe()
      if (this.token !== token) return
      if (user) {
        this.setUser(user)
        confirmReauthenticationDraftIdentity(this.user.id, this.isAdmin)
        void this.loadIdentityPasswordStatus()
      }
      else this.clear()
    },
    clear(options: { preserveReauthenticationDrafts?: boolean } = {}) {
      if (options.preserveReauthenticationDrafts) invalidateReauthenticationDraftIdentity()
      else clearReauthenticationDrafts()
      clearAuthReturnPath()
      this.reauthRequired = false
      this.passwordLoginAvailable = null
      this.sessionEpoch++
      resetAuthSessionExpired()
      clearTrackedTasks()
      this.token = ''
      this.user = { id: 0, email: '', isAdmin: false }
	  this.announcements = []
	  this.announcementUnreadCount = 0
      this.systemStatusLoadedAt = 0
      clearAuthToken()
    }
  }
})
