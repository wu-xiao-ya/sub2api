<template>
  <AppLayout>
    <section class="card p-6">
      <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('admin.subscriptionPurchases.title') }}</h1>
      <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.subscriptionPurchases.description') }}</p>
      <RouterLink class="btn btn-primary mt-5 inline-flex" to="/admin/subscription-purchases">
        {{ t('nav.subscriptionPurchases') }}
      </RouterLink>
    </section>
  </AppLayout>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'

const { t } = useI18n()
</script>
