/**
 * Sub2API native admin subscription endpoints are retired.
 *
 * Starlight subscription products are managed through the payment plan APIs,
 * and user entitlements are immutable subscription purchase snapshots. This
 * module intentionally exports no network client so deleted /admin/subscriptions
 * routes cannot be called accidentally by future UI code.
 */
export const nativeAdminSubscriptionsRetired = true as const

export default Object.freeze({ nativeAdminSubscriptionsRetired })
