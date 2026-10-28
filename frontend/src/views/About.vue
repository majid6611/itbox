<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api/client'
import { useSettingsStore } from '../stores/settings'
import Icon from '../components/Icon.vue'
import { timeAgo } from '../utils/time'

const settings = useSettingsStore()

interface Licence {
  install_id: string
  licence_key: string
  licensed: boolean
  send_monitoring: boolean
  monitor_url: string
  sending: boolean
  last_sent_at: string
  last_error: string
}
const licence = ref<Licence | null>(null)
const key = ref('')
const sendMonitoring = ref(true)
const busy = ref(false)
const message = ref('')
const preview = ref('')
const loading = ref(true)
const loadError = ref('')
const failed = ref(false)
const previewBusy = ref(false)

function load(l: Licence) {
  licence.value = l
  key.value = l.licence_key
  sendMonitoring.value = l.send_monitoring
}
// The API answers errors as {"detail": "..."} — show just the sentence.
function reason(e: unknown) {
  const raw = e instanceof Error ? e.message : String(e)
  try { return JSON.parse(raw).detail ?? raw } catch { return raw }
}
async function saveLicence() {
  busy.value = true
  message.value = ''
  failed.value = false
  try {
    load(await api.post<Licence>('/licence', { licence_key: key.value, send_monitoring: sendMonitoring.value }))
    message.value = 'Saved.'
    if (preview.value) await fetchPreview()
  } catch (e) { failed.value = true; message.value = reason(e) }
  busy.value = false
}
async function sendNow() {
  busy.value = true
  message.value = ''
  failed.value = false
  try {
    const r = await api.post<{ licence: Licence }>('/licence/send')
    load(r.licence)
    message.value = 'Report sent.'
  } catch (e) {
    failed.value = true
    message.value = reason(e)
    try { load(await api.get<Licence>('/licence')) } catch { /* keep what is shown */ }
  }
  busy.value = false
}
async function fetchPreview() {
  previewBusy.value = true
  try { preview.value = JSON.stringify(await api.get('/licence/preview'), null, 2) }
  catch (e) { failed.value = true; message.value = reason(e) }
  finally { previewBusy.value = false }
}
async function showSent() {
  if (preview.value) { preview.value = ''; return }
  await fetchPreview()
}
async function fetchAbout() {
  loading.value = true
  loadError.value = ''
  const results = await Promise.allSettled([settings.fetch(), api.get<Licence>('/licence')])
  if (results[0].status === 'rejected') loadError.value = 'Installation details are currently unavailable.'
  if (results[1].status === 'fulfilled') load(results[1].value)
  else loadError.value += ' Licence details could not be loaded. Try again.'
  loading.value = false
}
onMounted(fetchAbout)

// Placeholder — replace with the real support contact before shipping to
// anyone outside this dev environment.
const SUPPORT_EMAIL = 'support@example.com'
</script>

<template>
  <div class="about-page">
    <header class="page-heading"><span class="eyebrow">THE PLATFORM BEHIND YOUR WORKSPACE</span><h1>About IT Platform</h1><p>Your company's tools, together in a workspace you control.</p></header>

    <section class="card introduction" aria-labelledby="intro-title">
      <div class="product-mark"><Icon name="mark" :size="34" /></div>
      <div class="intro-copy"><div class="product-title"><h2 id="intro-title">IT Platform</h2><span v-if="settings.platformVersion" class="version">v{{ settings.platformVersion }}</span></div><p>A self-hosted home for your team's services. Simple to manage, built to grow with you.</p>
      <div class="traits"><span>Self-hosted</span><span>Open source</span><span>No per-seat fees</span></div></div>
      <a class="source-link" href="https://github.com/majid6611/itbox" target="_blank" rel="noopener noreferrer">View source <Icon name="arrow" :size="16" /></a>
    </section>

    <div v-if="loadError" class="load-error" role="alert"><span>{{ loadError }}</span><button class="secondary" :disabled="loading" @click="fetchAbout">{{ loading ? 'Loading…' : 'Try again' }}</button></div>
    <div class="about-grid">
      <div class="primary-column">
        <section class="card section" aria-labelledby="ownership-title">
          <div class="section-heading"><span class="section-icon"><Icon name="modules" :size="20" /></span><div><h2 id="ownership-title">Your infrastructure. Your choice.</h2><p>Free to run, inspect and make your own.</p></div></div>
          <p>IT Platform is free, open-source software. Host it on your own infrastructure and modify it for your company without per-seat fees or vendor lock-in.</p>
          <a class="text-link" href="https://github.com/majid6611/itbox" target="_blank" rel="noopener noreferrer">Explore the project on GitHub <Icon name="arrow" :size="15" /></a>
        </section>

        <section v-if="licence" class="card section licence-section" aria-labelledby="licence-title">
          <div class="section-heading"><span class="section-icon"><Icon name="logs" :size="20" /></span><div><h2 id="licence-title">Licence &amp; monitoring</h2><p>Manage health reporting for this installation.</p></div><span class="pill" :class="licence.licensed ? 'pill-good' : 'pill-neutral'">{{ licence.licensed ? 'Licensed' : 'No licence' }}</span></div>
          <p>A licence enables health reporting to help support spot problems and troubleshoot your installation.</p>
          <form @submit.prevent="saveLicence">
            <label class="field-label" for="licence-key">Licence key</label>
            <input id="licence-key" v-model="key" type="password" autocomplete="off" spellcheck="false" placeholder="Paste your licence key" :disabled="busy" />
            <label class="monitoring-toggle"><input v-model="sendMonitoring" type="checkbox" :disabled="busy" /><span><strong>Send monitoring data</strong><small>Share system health reports when a licence is configured.</small></span></label>
            <div class="report-status" role="status"><span class="pill" :class="licence.sending ? 'pill-good' : 'pill-neutral'">{{ licence.sending ? 'Sending' : 'Not sending' }}</span><span v-if="!licence.licensed">Enter a licence key to enable reporting.</span><span v-else-if="!licence.send_monitoring">Monitoring is switched off.</span><span v-else-if="licence.monitor_url === 'off'">Disabled by the installation administrator.</span><span v-else-if="licence.last_error" class="error-text">{{ licence.last_error }}</span><span v-else-if="licence.last_sent_at">Last report {{ timeAgo(licence.last_sent_at) }}.</span><span v-else>The first report will be sent within a few minutes.</span></div>
            <div class="actions"><button type="submit" :disabled="busy">{{ busy ? 'Working…' : 'Save changes' }}</button><button type="button" class="secondary" :disabled="busy || !licence.sending" @click="sendNow">Send report now</button><button type="button" class="preview-button secondary" :disabled="busy || previewBusy" :aria-expanded="!!preview" aria-controls="report-preview" @click="showSent">{{ previewBusy ? 'Loading…' : preview ? 'Hide report' : 'Preview report' }}</button></div>
          </form>
          <p v-if="message" class="feedback" :class="{ failed }" :role="failed ? 'alert' : 'status'">{{ message }}</p>
          <div v-if="preview" id="report-preview" class="preview"><div class="preview-heading">Report preview <span>JSON</span></div><pre class="sent" tabindex="0" aria-label="Monitoring report contents">{{ preview }}</pre></div>
        </section>
        <section v-else-if="loading" class="card section" role="status">Loading installation details…</section>
      </div>

      <aside class="secondary-column">
        <section class="card section" aria-labelledby="installation-title"><h2 id="installation-title">This installation</h2><dl class="installation-details"><div><dt>Version</dt><dd>{{ settings.platformVersion ? `v${settings.platformVersion}` : loading ? 'Loading…' : 'Unavailable' }}</dd></div><div><dt>Domain</dt><dd>{{ settings.baseDomain || (loading ? 'Loading…' : 'Not configured') }}</dd></div><div v-if="licence"><dt>Installation ID</dt><dd class="installation-id">{{ licence.install_id }}</dd></div></dl></section>
        <section class="card section privacy" aria-labelledby="privacy-title"><span class="eyebrow">REPORTING AT A GLANCE</span><h2 id="privacy-title">Know what you share</h2><p>Monitoring reports include CPU, memory and disk metrics, health checks and system log warnings.</p><p>People's names, files, messages and calls are excluded. Use the report preview to inspect the data.</p></section>
        <section class="card section support" aria-labelledby="support-title"><span class="section-icon"><Icon name="chat" :size="20" /></span><h2 id="support-title">A little help goes a long way.</h2><p>Paid support is available for setup, upgrades and troubleshooting.</p><a v-if="SUPPORT_EMAIL !== 'support@example.com'" class="text-link" :href="`mailto:${SUPPORT_EMAIL}`">Contact support <Icon name="arrow" :size="15" /></a><a v-else class="text-link" href="https://github.com/majid6611/itbox/issues" target="_blank" rel="noopener noreferrer">Project issues <Icon name="arrow" :size="15" /></a></section>
      </aside>
    </div>
  </div>
</template>

<style scoped>
.about-page { max-width: 1160px; margin: 0 auto; }
.page-heading { margin-bottom: 1.75rem; }
.eyebrow { font-size: .65rem; font-weight: 700; letter-spacing: .13em; color: var(--accent); }
h1 { margin: .4rem 0; }
.page-heading p, .intro-copy p { color: var(--text-dim); margin: 0; font-size: .9rem; }
.introduction { display: flex; align-items: center; gap: 1.5rem; padding: 2rem; margin-bottom: 1.5rem; border-top: 3px solid var(--accent); }
.product-mark { width: 76px; height: 76px; border-radius: 20px; background: var(--accent-soft); color: var(--accent); display: grid; place-items: center; flex-shrink: 0; }
.intro-copy { flex: 1; min-width: 0; }
.product-title { display: flex; align-items: center; gap: .75rem; flex-wrap: wrap; margin-bottom: .45rem; }
.product-title h2 { font-size: 1.4rem; margin: 0; }
.version { color: var(--accent); background: var(--accent-soft); border-radius: 6px; padding: .2rem .5rem; font-size: .75rem; overflow-wrap: anywhere; }
.traits { display: flex; flex-wrap: wrap; gap: .5rem 1.2rem; margin-top: 1rem; font-size: .72rem; font-weight: 600; color: var(--text-dim); }
.traits span::before { content: ''; display: inline-block; width: 5px; height: 5px; border-radius: 50%; background: var(--accent); margin-right: .45rem; vertical-align: middle; }
.source-link, .text-link { display: inline-flex; align-items: center; gap: .5rem; font-size: .8rem; font-weight: 600; }
.source-link { border: 1px solid var(--border-strong); border-radius: 8px; padding: .65rem .85rem; white-space: nowrap; }
.source-link:hover { background: var(--surface-hover); }
.about-grid { display: grid; grid-template-columns: minmax(0, 1fr) 300px; gap: 1.5rem; align-items: start; }
.primary-column, .secondary-column { display: grid; gap: 1.5rem; min-width: 0; }
.section { padding: 1.5rem; min-width: 0; }
.section h2 { font-size: 1rem; margin: 0 0 .6rem; }
.section p { font-size: .85rem; color: var(--text-dim); line-height: 1.75; margin: 0 0 1rem; }
.section p:last-child { margin-bottom: 0; }
.section-heading { display: flex; gap: .85rem; align-items: center; margin-bottom: 1.25rem; flex-wrap: wrap; }
.section-heading > div { flex: 1; min-width: 150px; }
.section-heading h2 { margin: 0 0 .2rem; }
.section-heading p { margin: 0; font-size: .78rem; }
.section-icon { display: inline-flex; align-items: center; justify-content: center; padding: .65rem; background: var(--accent-soft); color: var(--accent); border-radius: 10px; }
.field-label { display: block; font-size: .8rem; font-weight: 600; margin: 1.5rem 0 .5rem; }
#licence-key { width: 100%; min-height: 44px; }
.monitoring-toggle { display: flex; gap: .75rem; align-items: flex-start; padding: 1rem 0; cursor: pointer; }
.monitoring-toggle input { margin: .25rem 0 0; flex-shrink: 0; }
.monitoring-toggle strong, .monitoring-toggle small { display: block; }
.monitoring-toggle strong { font-size: .83rem; font-weight: 600; }
.monitoring-toggle small { margin-top: .2rem; color: var(--text-dim); font-size: .75rem; }
.report-status { display: flex; gap: .65rem; align-items: center; flex-wrap: wrap; padding: .85rem; background: var(--bg); border-radius: 8px; font-size: .78rem; color: var(--text-dim); overflow-wrap: anywhere; }
.actions { display: flex; flex-wrap: wrap; gap: .6rem; border-top: 1px solid var(--border); padding-top: 1.25rem; margin-top: 1.25rem; }
.actions button { font-size: .78rem; }
.preview-button { margin-left: auto; }
.installation-details { margin: 1rem 0 0; }
.installation-details > div { padding: .8rem 0; border-top: 1px solid var(--border); }
dt { color: var(--text-dim); font-size: .72rem; margin-bottom: .3rem; }
dd { font-size: .83rem; margin: 0; overflow-wrap: anywhere; }
.installation-id { font-family: var(--font-mono); font-size: .72rem; line-height: 1.7; }
.privacy { background: var(--accent-soft); box-shadow: none; }
.privacy h2 { margin-top: .6rem; }
.support .section-icon { margin-bottom: 1rem; }
.feedback { margin-top: 1rem !important; padding: .75rem; border-radius: 8px; background: var(--success-bg); color: var(--success-text) !important; }
.feedback.failed, .load-error { background: var(--danger-bg); color: var(--danger-text) !important; }
.error-text { color: var(--danger-text); }
.load-error { display: flex; align-items: center; justify-content: space-between; gap: 1rem; padding: 1rem; margin-bottom: 1.5rem; border-radius: 10px; font-size: .85rem; }
.preview { margin-top: 1rem; border: 1px solid var(--border); border-radius: 9px; overflow: hidden; }
.preview-heading { display: flex; justify-content: space-between; padding: .65rem .85rem; background: var(--surface-hover); font-size: .75rem; font-weight: 600; }
.preview-heading span { color: var(--text-dim); font-family: var(--font-mono); }
.sent { max-height: 20rem; overflow: auto; margin: 0; padding: 1rem; font-size: .72rem; background: var(--bg); font-family: var(--font-mono); }
@media (max-width: 1100px) { .about-grid { grid-template-columns: minmax(0, 1fr) 260px; gap: 1rem; } .source-link { align-self: flex-start; } .introduction { flex-wrap: wrap; } }
@media (max-width: 900px) { .about-grid { grid-template-columns: 1fr; } .secondary-column { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 1rem; } .support { grid-column: 1 / -1; } }
@media (max-width: 600px) { .introduction { padding: 1.25rem; gap: 1rem; } .product-mark { width: 52px; height: 52px; border-radius: 14px; } .intro-copy { flex-basis: 65%; } .source-link { width: 100%; justify-content: center; } .secondary-column { grid-template-columns: 1fr; } .section { padding: 1.2rem; } .preview-button { margin-left: 0; } .actions button { flex: 1 1 auto; } }
</style>
