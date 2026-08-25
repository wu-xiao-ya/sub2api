/**
 * Admin API barrel export
 * Centralized exports for all admin API modules
 */

import dashboardAPI from './dashboard'
import usersAPI from './users'
import groupsAPI from './groups'
import accountsAPI from './accounts'
import proxiesAPI from './proxies'
import redeemAPI from './redeem'
import promoAPI from './promo'
import announcementsAPI from './announcements'
import groupPromotionsAPI from './groupPromotions'
import settingsAPI from './settings'
import systemAPI from './system'
import usageAPI from './usage'
import geminiAPI from './gemini'
import antigravityAPI from './antigravity'
import grokAPI from './grok'
import userAttributesAPI from './userAttributes'
import opsAPI from './ops'
import errorPassthroughAPI from './errorPassthrough'
import dataManagementAPI from './dataManagement'
import apiKeysAPI from './apiKeys'
import scheduledTestsAPI from './scheduledTests'
import backupAPI from './backup'
import tlsFingerprintProfileAPI from './tlsFingerprintProfile'
import channelsAPI from './channels'
import channelMonitorAPI from './channelMonitor'
import channelMonitorTemplateAPI from './channelMonitorTemplate'
import adminPaymentAPI from './payment'
import affiliatesAPI from './affiliates'
import riskControlAPI from './riskControl'
import adminComplianceAPI from './compliance'
import adminAccountContributionsAPI from './accountContributions'
import auditAPI from './audit'
import financeAPI from './finance'
import subscriptionPurchasesAPI from './subscriptionPurchases'

/**
 * Unified admin API object for convenient access
 */
export const adminAPI = {
  dashboard: dashboardAPI,
  users: usersAPI,
  groups: groupsAPI,
  accounts: accountsAPI,
  proxies: proxiesAPI,
  redeem: redeemAPI,
  promo: promoAPI,
  announcements: announcementsAPI,
  groupPromotions: groupPromotionsAPI,
  settings: settingsAPI,
  system: systemAPI,
  usage: usageAPI,
  gemini: geminiAPI,
  antigravity: antigravityAPI,
  grok: grokAPI,
  userAttributes: userAttributesAPI,
  ops: opsAPI,
  errorPassthrough: errorPassthroughAPI,
  dataManagement: dataManagementAPI,
  apiKeys: apiKeysAPI,
  scheduledTests: scheduledTestsAPI,
  backup: backupAPI,
  tlsFingerprintProfiles: tlsFingerprintProfileAPI,
  channels: channelsAPI,
  channelMonitor: channelMonitorAPI,
  channelMonitorTemplate: channelMonitorTemplateAPI,
  payment: adminPaymentAPI,
  affiliates: affiliatesAPI,
  riskControl: riskControlAPI,
  compliance: adminComplianceAPI,
  accountContributions: adminAccountContributionsAPI,
  audit: auditAPI,
  finance: financeAPI,
  subscriptionPurchases: subscriptionPurchasesAPI
}

export {
  dashboardAPI,
  usersAPI,
  groupsAPI,
  accountsAPI,
  proxiesAPI,
  redeemAPI,
  promoAPI,
  announcementsAPI,
  groupPromotionsAPI,
  settingsAPI,
  systemAPI,
  usageAPI,
  geminiAPI,
  antigravityAPI,
  grokAPI,
  userAttributesAPI,
  opsAPI,
  errorPassthroughAPI,
  dataManagementAPI,
  apiKeysAPI,
  scheduledTestsAPI,
  backupAPI,
  tlsFingerprintProfileAPI,
  channelsAPI,
  channelMonitorAPI,
  channelMonitorTemplateAPI,
  adminPaymentAPI,
  affiliatesAPI,
  riskControlAPI,
  adminComplianceAPI,
  adminAccountContributionsAPI,
  auditAPI,
  financeAPI,
  subscriptionPurchasesAPI
}

export default adminAPI

// Re-export types used by components
export type { AuditLog, AuditLogQuery, AuditLogListResponse } from './audit'
export type { BalanceHistoryItem } from './users'
export type { ErrorPassthroughRule, CreateRuleRequest, UpdateRuleRequest } from './errorPassthrough'
export type { BackupAgentHealth, DataManagementConfig } from './dataManagement'
export type { TLSFingerprintProfile, CreateProfileRequest, UpdateProfileRequest } from './tlsFingerprintProfile'
export type { ContentModerationConfig, ContentModerationLog, ModerationMode } from './riskControl'
export type {
  FinanceLedgerDirection,
  FinanceLedgerEntry,
  FinanceLedgerQuery,
  FinanceLedgerResponse,
  FinanceLedgerSource,
  FinanceLedgerSummary,
} from './finance'
