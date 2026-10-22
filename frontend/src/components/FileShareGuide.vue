<script setup lang="ts">
import { computed, ref } from 'vue'

const props = defineProps<{
  // Host name of the file server, e.g. "fileshare-webdav.example.com".
  hostname: string
  // The signed-in employee's username, when known (employee page).
  username?: string
}>()

// The address is built with the port this site was opened on, the same way
// the Module Store builds its links.
const port = computed(() => (window.location.port ? `:${window.location.port}` : ''))
const address = computed(() => `${window.location.protocol}//${props.hostname}${port.value}/`)
const davAddress = computed(() => `${window.location.protocol === 'https:' ? 'davs' : 'dav'}://${props.hostname}${port.value}/`)

const copied = ref('')
async function copy(text: string, what: string) {
  try {
    await navigator.clipboard.writeText(text)
    copied.value = what
    setTimeout(() => (copied.value = ''), 2000)
  } catch {
    // Clipboard blocked (plain http page, old browser): the text is
    // selectable on its own, so nothing to do.
  }
}

const tab = ref<'windows' | 'mac' | 'linux' | 'phone'>('windows')
</script>

<template>
  <div class="guide">
    <div class="card address-card">
      <div class="row">
        <span class="label">File server address</span>
        <code>{{ address }}</code>
        <button type="button" class="secondary small" @click="copy(address, 'address')">
          {{ copied === 'address' ? 'Copied' : 'Copy' }}
        </button>
      </div>
      <div class="row">
        <span class="label">Username</span>
        <code v-if="username">{{ username }}</code>
        <span v-else>your company username</span>
      </div>
      <div class="row">
        <span class="label">Password</span>
        <span>your company password (the same one you use for everything else)</span>
      </div>
    </div>

    <p class="hint">
      You'll find your own folder (named after your username), your team's folder, and a
      <strong>shared</strong> folder everyone can use. Opening the address in a web browser only shows a login box
      — connect it as a network folder instead:
    </p>

    <div class="tabs">
      <button type="button" :class="{ active: tab === 'windows' }" @click="tab = 'windows'">Windows</button>
      <button type="button" :class="{ active: tab === 'mac' }" @click="tab = 'mac'">Mac</button>
      <button type="button" :class="{ active: tab === 'linux' }" @click="tab = 'linux'">Linux</button>
      <button type="button" :class="{ active: tab === 'phone' }" @click="tab = 'phone'">Phone / tablet</button>
    </div>

    <div class="card steps" v-if="tab === 'windows'">
      <ol>
        <li>Open <strong>File Explorer</strong>, right-click <strong>This PC</strong> and choose <strong>Map network drive…</strong></li>
        <li>
          In <strong>Folder</strong>, paste <code>{{ address }}</code>
          <button type="button" class="secondary small" @click="copy(address, 'win')">{{ copied === 'win' ? 'Copied' : 'Copy' }}</button>
        </li>
        <li>Tick <strong>Connect using different credentials</strong> and click <strong>Finish</strong>.</li>
        <li>Enter your company username and password. The folders appear as a drive.</li>
      </ol>
      <p class="hint small">
        If Windows says the password is wrong when it isn't: Windows blocks passwords over a plain (non-https) address
        by default. Ask your administrator, or use the free app <strong>Cyberduck</strong> or <strong>WinSCP</strong> with
        protocol "WebDAV (HTTP)" and the address above.
      </p>
    </div>

    <div class="card steps" v-else-if="tab === 'mac'">
      <ol>
        <li>In <strong>Finder</strong>, choose <strong>Go → Connect to Server…</strong> (or press <kbd>⌘</kbd> <kbd>K</kbd>).</li>
        <li>
          Paste <code>{{ address }}</code>
          <button type="button" class="secondary small" @click="copy(address, 'mac')">{{ copied === 'mac' ? 'Copied' : 'Copy' }}</button>
          and click <strong>Connect</strong>.
        </li>
        <li>Choose <strong>Registered User</strong>, enter your company username and password, and click <strong>Connect</strong>.</li>
        <li>The share opens like a disk in Finder's sidebar.</li>
      </ol>
      <p class="hint small">macOS warns that the connection isn't encrypted when the address starts with http — that's expected on the office network.</p>
    </div>

    <div class="card steps" v-else-if="tab === 'linux'">
      <ol>
        <li>Open your <strong>Files</strong> app (Nemo, Nautilus, Dolphin…).</li>
        <li>
          Press <kbd>Ctrl</kbd> <kbd>L</kbd> and type <code>{{ davAddress }}</code>
          <button type="button" class="secondary small" @click="copy(davAddress, 'linux')">{{ copied === 'linux' ? 'Copied' : 'Copy' }}</button>
          then press Enter.
        </li>
        <li>Enter your company username and password, and tick "Remember" if you like.</li>
      </ol>
      <p class="hint small">
        Or from a terminal: <code>rclone</code> with a WebDAV remote at <code>{{ address }}</code>, vendor "other".
      </p>
    </div>

    <div class="card steps" v-else>
      <ol>
        <li>
          Install a file app that supports <strong>WebDAV</strong> — for example <strong>Documents</strong> (iPhone/iPad),
          or <strong>Solid Explorer</strong> / <strong>CX File Explorer</strong> (Android).
        </li>
        <li>Add a new connection of type <strong>WebDAV</strong> and use the address <code>{{ address }}</code></li>
        <li>Enter your company username and password.</li>
      </ol>
      <p class="hint small">
        Away from the office? Connect to the company VPN first — the address works the same way afterwards.
      </p>
    </div>
  </div>
</template>

<style scoped>
.guide {
  max-width: 44rem;
}
.address-card {
  padding: 1rem 1.2rem;
  display: flex;
  flex-direction: column;
  gap: 0.55rem;
  margin-bottom: 1rem;
}
.row {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  flex-wrap: wrap;
  font-size: 0.92rem;
}
.label {
  width: 9.5rem;
  font-size: 0.72rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-faint);
}
code {
  user-select: all;
  word-break: break-all;
}
.hint {
  font-size: 0.9rem;
  color: var(--text-dim);
  line-height: 1.55;
  margin: 0 0 1rem;
}
.hint.small {
  font-size: 0.82rem;
  margin: 0.75rem 0 0;
}
.tabs {
  display: flex;
  gap: 0.4rem;
  flex-wrap: wrap;
  margin-bottom: 0.75rem;
}
.tabs button {
  background: none;
  border: 1px solid var(--border);
  color: var(--text-dim);
  padding: 0.4rem 0.9rem;
  border-radius: 7px;
  cursor: pointer;
  font-weight: 500;
}
.tabs button.active {
  background: var(--accent-soft);
  color: var(--accent);
  border-color: var(--accent);
}
.steps {
  padding: 1rem 1.2rem;
}
.steps ol {
  margin: 0;
  padding-left: 1.2rem;
  display: flex;
  flex-direction: column;
  gap: 0.6rem;
  font-size: 0.92rem;
  line-height: 1.5;
}
.small {
  font-size: 0.78rem;
  padding: 0.15rem 0.55rem;
  margin-left: 0.4rem;
}
kbd {
  border: 1px solid var(--border-strong);
  border-radius: 4px;
  padding: 0 0.35rem;
  font-size: 0.8rem;
}
@media (max-width: 700px) {
  .label {
    width: 100%;
  }
}
</style>
