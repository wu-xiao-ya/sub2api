<template>
  <AppLayout>
    <div class="mx-auto max-w-2xl">
      <div class="card p-8 text-center">
        <div
          class="mx-auto flex h-14 w-14 items-center justify-center rounded-full"
          :class="success ? 'bg-emerald-100 dark:bg-emerald-900/30' : 'bg-primary-100 dark:bg-primary-900/30'"
        >
          <Icon
            :name="success ? 'check' : 'refresh'"
            size="lg"
            :class="[
              success ? 'text-emerald-600 dark:text-emerald-400' : 'text-primary-600 dark:text-primary-400',
              processing && !success ? 'animate-spin' : ''
            ]"
          />
        </div>

        <h2 class="mt-5 text-xl font-semibold text-gray-900 dark:text-white">
          {{ title }}
        </h2>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
          {{ message }}
        </p>

        <div v-if="errorMessage" class="mt-5 rounded-xl border border-red-200 bg-red-50 p-4 text-left text-sm text-red-700 dark:border-red-900/40 dark:bg-red-900/20 dark:text-red-300">
          {{ errorMessage }}
        </div>

        <div class="mt-6 flex justify-center gap-3">
          <RouterLink class="btn btn-secondary" to="/account-contributions">
            {{ t('accountContributions.backToList') }}
          </RouterLink>
          <button v-if="canRetry" class="btn btn-primary" @click="submit">
            {{ t('accountContributions.callback.retry') }}
          </button>
        </div>
      </div>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import accountContributionsAPI from '@/api/accountContributions'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const SESSION_ID_KEY = 'openai_contribution_session_id'
const STATE_KEY = 'openai_contribution_state'
const REDIRECT_URI_KEY = 'openai_contribution_redirect_uri'
const OPENAI_DEFAULT_REDIRECT_URI = 'http://localhost:1455/auth/callback'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()

const processing = ref(false)
const success = ref(false)
const errorMessage = ref('')

const title = computed(() => {
  if (success.value) return t('accountContributions.callback.successTitle')
  if (errorMessage.value) return t('accountContributions.callback.failedTitle')
  return t('accountContributions.callback.processingTitle')
})

const message = computed(() => {
  if (success.value) return t('accountContributions.callback.successMessage')
  if (errorMessage.value) return t('accountContributions.callback.failedMessage')
  return t('accountContributions.callback.processingMessage')
})

const canRetry = computed(() => !processing.value && !success.value && !!route.query.code)

function firstQueryValue(value: unknown): string {
  if (Array.isArray(value)) return String(value[0] ?? '')
  return String(value ?? '')
}

function clearStoredOAuthState(): void {
  sessionStorage.removeItem(SESSION_ID_KEY)
  sessionStorage.removeItem(STATE_KEY)
  sessionStorage.removeItem(REDIRECT_URI_KEY)
}

async function submit(): Promise<void> {
  if (processing.value || success.value) return
  processing.value = true
  errorMessage.value = ''

  try {
    const upstreamError = firstQueryValue(route.query.error)
    if (upstreamError) {
      const description = firstQueryValue(route.query.error_description)
      throw new Error(description || upstreamError)
    }

    const code = firstQueryValue(route.query.code)
    const state = firstQueryValue(route.query.state)
    const sessionID = sessionStorage.getItem(SESSION_ID_KEY) || ''
    const expectedState = sessionStorage.getItem(STATE_KEY) || ''
    const redirectURI = sessionStorage.getItem(REDIRECT_URI_KEY) || OPENAI_DEFAULT_REDIRECT_URI

    if (!code) throw new Error(t('accountContributions.callback.missingCode'))
    if (!state) throw new Error(t('accountContributions.callback.missingState'))
    if (!sessionID) throw new Error(t('accountContributions.callback.missingSession'))
    if (expectedState && expectedState !== state) {
      throw new Error(t('accountContributions.callback.stateMismatch'))
    }

    await accountContributionsAPI.submitOpenAI({
      session_id: sessionID,
      code,
      state,
      redirect_uri: redirectURI
    })

    clearStoredOAuthState()
    success.value = true
    appStore.showSuccess(t('accountContributions.callback.submitted'))
    setTimeout(() => {
      void router.replace('/account-contributions')
    }, 1200)
  } catch (error) {
    errorMessage.value = extractApiErrorMessage(error, t('accountContributions.callback.submitFailed'))
  } finally {
    processing.value = false
  }
}

onMounted(() => {
  void submit()
})
</script>
