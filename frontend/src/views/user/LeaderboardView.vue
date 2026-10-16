<template>
  <AppLayout>
    <section class="min-w-0 space-y-4">
      <header class="border-b border-gray-200 pb-4 dark:border-dark-700">
        <h1 class="text-xl font-semibold text-gray-900 dark:text-gray-100">
          {{ t('leaderboard.title') }}
        </h1>
      </header>
      <UserLeaderboard />
    </section>
  </AppLayout>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import UserLeaderboard from '@/components/user/UserLeaderboard.vue'
import AppLayout from '@/components/layout/AppLayout.vue'

const { t } = useI18n()
</script>
