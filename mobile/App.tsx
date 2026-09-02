import { useCallback, useEffect, useRef, useState } from 'react';
import {
  ActivityIndicator,
  AppState,
  Pressable,
  RefreshControl,
  SafeAreaView,
  ScrollView,
  StyleSheet,
  Text,
  View,
} from 'react-native';
import { StatusBar } from 'expo-status-bar';

import { gql, GraphQLError } from './src/api';

type User = { id: string; name: string };
type Tier = 'PINK' | 'BLUE' | 'GREEN';
type Contact = { id: string; user: User; tier: Tier };
type TierCapacity = { tier: Tier; used: number; cap: number };
type Capacity = { budgetUsed: number; budgetCap: number; tiers: TierCapacity[] };
type Request = { id: string; from: User; to: User; tier: Tier };

const TIERS: Tier[] = ['PINK', 'BLUE', 'GREEN'];

// Poll while the app is open so another device’s send/accept shows up without a manual reload.
const POLL_INTERVAL_MS = 3000;

const TIER_META: Record<Tier, { label: string; color: string; bg: string }> = {
  PINK: { label: 'Pink', color: '#b0126a', bg: '#fde7f3' },
  BLUE: { label: 'Blue', color: '#1565c0', bg: '#e3f2fd' },
  GREEN: { label: 'Green', color: '#2e7d32', bg: '#e8f5e9' },
};

const DASHBOARD_QUERY = `
  query {
    users { id name }
    capacity { budgetUsed budgetCap tiers { tier used cap } }
    contacts { id user { id name } tier }
    incomingRequests { id from { id name } tier }
  }
`;

type DashboardData = {
  users: User[];
  capacity: Capacity;
  contacts: Contact[];
  incomingRequests: Request[];
};

function ProgressBar({
  used,
  cap,
  color,
  fullColor = '#9a3b2e',
}: {
  used: number;
  cap: number;
  color: string;
  fullColor?: string;
}) {
  const pct = cap > 0 ? Math.min((used / cap) * 100, 100) : 0;
  const isFull = used >= cap;
  return (
    <View style={styles.progressTrack}>
      <View
        style={[
          styles.progressFill,
          { width: `${pct}%`, backgroundColor: isFull ? fullColor : color },
        ]}
      />
    </View>
  );
}

function TierBadge({ tier, small }: { tier: Tier; small?: boolean }) {
  const meta = TIER_META[tier];
  return (
    <View style={[styles.tierBadge, { backgroundColor: meta.bg }, small && styles.tierBadgeSmall]}>
      <Text style={[styles.tierBadgeText, { color: meta.color }, small && styles.tierBadgeTextSmall]}>
        {meta.label}
      </Text>
    </View>
  );
}

export default function App() {
  const [users, setUsers] = useState<User[]>([]);
  const [capacity, setCapacity] = useState<Capacity | null>(null);
  const [contacts, setContacts] = useState<Contact[]>([]);
  const [incoming, setIncoming] = useState<Request[]>([]);
  const [me, setMe] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionSuccess, setActionSuccess] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [busy, setBusy] = useState(false);

  const [sendTo, setSendTo] = useState<string | null>(null);
  const [sendTier, setSendTier] = useState<Tier>('BLUE');

  const currentUser = users.find((u) => u.id === me);

  const refresh = useCallback(async (userId: string, silent = false) => {
    try {
      const data = await gql<DashboardData>(DASHBOARD_QUERY, {}, userId);
      setUsers(data.users);
      setCapacity(data.capacity);
      setContacts(data.contacts);
      setIncoming(data.incomingRequests);
      if (!silent) setError(null);
    } catch (e) {
      if (!silent) throw e;
    }
  }, []);

  const load = useCallback(async () => {
    try {
      setError(null);
      if (!me) {
        const boot = await gql<{ users: User[] }>(`{ users { id name } }`);
        setUsers(boot.users);
        setMe(boot.users[0]?.id ?? null);
        return;
      }
      await refresh(me);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, [me, refresh]);

  useEffect(() => {
    setLoading(true);
    void load();
  }, [me, load]);

  useEffect(() => {
    if (!actionSuccess) return;
    const t = setTimeout(() => setActionSuccess(null), 3000);
    return () => clearTimeout(t);
  }, [actionSuccess]);

  const busyRef = useRef(busy);
  busyRef.current = busy;

  useEffect(() => {
    if (!me || loading) return;

    const poll = () => {
      if (AppState.currentState !== 'active' || busyRef.current) return;
      void refresh(me, true);
    };

    const intervalId = setInterval(poll, POLL_INTERVAL_MS);
    const appStateSub = AppState.addEventListener('change', (state) => {
      if (state === 'active' && !busyRef.current) void refresh(me, true);
    });

    return () => {
      clearInterval(intervalId);
      appStateSub.remove();
    };
  }, [me, loading, refresh]);

  const onRefresh = useCallback(() => {
    if (!me) return;
    setRefreshing(true);
    void load();
  }, [me, load]);

  const switchUser = (userId: string) => {
    setMe(userId);
    setActionError(null);
    setActionSuccess(null);
    setSendTo(null);
  };

  const runAction = async (fn: () => Promise<void>, successMessage: string) => {
    if (!me) return;
    setBusy(true);
    setActionError(null);
    setActionSuccess(null);
    try {
      await fn();
      await refresh(me);
      setActionSuccess(successMessage);
    } catch (e) {
      setActionError(e instanceof GraphQLError ? e.message : e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };

  const acceptRequest = (name: string, tier: Tier, requestId: string) =>
    runAction(
      async () => {
        await gql(`mutation($id: ID!) { acceptRequest(requestId: $id) { id } }`, { id: requestId }, me!);
      },
      `Added ${name} to ${TIER_META[tier].label}.`,
    );

  const declineRequest = (name: string, requestId: string) =>
    runAction(
      async () => {
        await gql(`mutation($id: ID!) { declineRequest(requestId: $id) { id } }`, { id: requestId }, me!);
      },
      `Declined request from ${name}.`,
    );

  const sendRequest = () => {
    if (!me || !sendTo) return;
    const recipient = users.find((u) => u.id === sendTo)?.name ?? 'user';
    runAction(
      async () => {
        await gql(
          `mutation($to: ID!, $tier: Tier!) { sendRequest(toUserId: $to, tier: $tier) { id } }`,
          { to: sendTo, tier: sendTier },
          me,
        );
        setSendTo(null);
      },
      `Request sent to ${recipient} (${TIER_META[sendTier].label}).`,
    );
  };

  const moveContact = (name: string, contactId: string, tier: Tier) =>
    runAction(
      async () => {
        await gql(
          `mutation($id: ID!, $tier: Tier!) { moveContact(contactId: $id, tier: $tier) { id } }`,
          { id: contactId, tier },
          me!,
        );
      },
      `Moved ${name} to ${TIER_META[tier].label}.`,
    );

  const removeContact = (name: string, contactId: string) =>
    runAction(
      async () => {
        await gql(`mutation($id: ID!) { removeContact(contactId: $id) }`, { id: contactId }, me!);
      },
      `Removed ${name}.`,
    );

  const otherUsers = users.filter((u) => u.id !== me);
  const contactsByTier = (tier: Tier) => contacts.filter((c) => c.tier === tier);
  const tierCap = (tier: Tier) => capacity?.tiers.find((t) => t.tier === tier);
  const budgetFull = capacity ? capacity.budgetUsed >= capacity.budgetCap : false;

  return (
    <SafeAreaView style={styles.screen}>
      <StatusBar style="dark" />

      {/* Header */}
      <View style={styles.header}>
        <View>
          <Text style={styles.appTitle}>Capacity</Text>
          {currentUser && (
            <Text style={styles.appSubtitle}>Acting as {currentUser.name}</Text>
          )}
        </View>
        {(loading || busy) && <ActivityIndicator color="#16605c" />}
      </View>

      <ScrollView
        contentContainerStyle={styles.body}
        refreshControl={
          <RefreshControl refreshing={refreshing} onRefresh={onRefresh} tintColor="#16605c" />
        }
      >
        {/* Feedback banners */}
        {error && (
          <View style={styles.bannerError}>
            <Text style={styles.bannerTitle}>Could not load</Text>
            <Text style={styles.bannerText}>{error}</Text>
            <Text style={styles.bannerHint}>Run `make up` then `make api`, then pull to refresh.</Text>
            <Pressable onPress={load} style={styles.bannerBtn}>
              <Text style={styles.bannerBtnText}>Retry</Text>
            </Pressable>
          </View>
        )}

        {actionError && (
          <View style={styles.bannerError}>
            <Text style={styles.bannerTitle}>Action failed</Text>
            <Text style={styles.bannerText}>{actionError}</Text>
            <Pressable onPress={() => setActionError(null)} style={styles.bannerBtnOutline}>
              <Text style={styles.bannerBtnOutlineText}>Dismiss</Text>
            </Pressable>
          </View>
        )}

        {actionSuccess && (
          <View style={styles.bannerSuccess}>
            <Text style={styles.bannerSuccessText}>{actionSuccess}</Text>
          </View>
        )}

        {/* User switcher */}
        <View style={styles.card}>
          <Text style={styles.sectionLabel}>Switch user</Text>
          <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.chipRow}>
            {users.map((u) => {
              const active = u.id === me;
              return (
                <Pressable
                  key={u.id}
                  onPress={() => switchUser(u.id)}
                  style={[styles.chip, active && styles.chipActive]}
                >
                  <Text style={[styles.chipText, active && styles.chipTextActive]}>{u.name}</Text>
                </Pressable>
              );
            })}
          </ScrollView>
        </View>

        {!loading && capacity && (
          <>
            {/* Capacity overview */}
            <View style={styles.card}>
              <Text style={styles.sectionLabel}>Capacity</Text>
              <View style={styles.budgetRow}>
                <Text style={styles.budgetLabel}>Total budget</Text>
                <Text style={[styles.budgetValue, budgetFull && styles.budgetValueFull]}>
                  {capacity.budgetUsed} / {capacity.budgetCap}
                </Text>
              </View>
              <ProgressBar
                used={capacity.budgetUsed}
                cap={capacity.budgetCap}
                color="#16605c"
              />
              {budgetFull && (
                <Text style={styles.fullHint}>Budget full — accepts may be refused.</Text>
              )}

              <View style={styles.tierCapList}>
                {capacity.tiers.map((t) => {
                  const meta = TIER_META[t.tier];
                  const full = t.used >= t.cap;
                  return (
                    <View key={t.tier} style={styles.tierCapRow}>
                      <View style={styles.tierCapHeader}>
                        <TierBadge tier={t.tier} small />
                        <Text style={[styles.tierCapCount, full && styles.tierCapCountFull]}>
                          {t.used} / {t.cap}
                        </Text>
                      </View>
                      <ProgressBar used={t.used} cap={t.cap} color={meta.color} />
                    </View>
                  );
                })}
              </View>
            </View>

            {/* Contacts by tier */}
            <View style={styles.card}>
              <Text style={styles.sectionLabel}>Contacts</Text>
              {contacts.length === 0 && (
                <Text style={styles.emptyText}>No contacts yet. Accept a request to add someone.</Text>
              )}
              {TIERS.map((tier) => {
                const cap = tierCap(tier);
                const tierContacts = contactsByTier(tier);
                const meta = TIER_META[tier];
                return (
                  <View
                    key={tier}
                    style={[styles.tierSection, { borderLeftColor: meta.color, backgroundColor: meta.bg }]}
                  >
                    <View style={styles.tierSectionHeader}>
                      <TierBadge tier={tier} />
                      <Text style={styles.tierSectionCount}>
                        {cap?.used ?? 0} / {cap?.cap ?? 0} seats
                      </Text>
                    </View>
                    {tierContacts.length === 0 ? (
                      <Text style={styles.emptyTextSmall}>Empty</Text>
                    ) : (
                      tierContacts.map((c) => (
                        <View key={c.id} style={styles.contactCard}>
                          <Text style={styles.contactName}>{c.user.name}</Text>
                          <View style={styles.contactActions}>
                            <Text style={styles.moveLabel}>Move to</Text>
                            <View style={styles.moveRow}>
                              {TIERS.filter((t) => t !== c.tier).map((t) => (
                                <Pressable
                                  key={t}
                                  disabled={busy}
                                  onPress={() => moveContact(c.user.name, c.id, t)}
                                  style={[styles.moveBtn, { borderColor: TIER_META[t].color }]}
                                >
                                  <Text style={[styles.moveBtnText, { color: TIER_META[t].color }]}>
                                    {TIER_META[t].label}
                                  </Text>
                                </Pressable>
                              ))}
                              <Pressable
                                disabled={busy}
                                onPress={() => removeContact(c.user.name, c.id)}
                                style={styles.removeBtn}
                              >
                                <Text style={styles.removeBtnText}>Remove</Text>
                              </Pressable>
                            </View>
                          </View>
                        </View>
                      ))
                    )}
                  </View>
                );
              })}
            </View>

            {/* Incoming requests */}
            <View style={styles.card}>
              <View style={styles.inboxHeader}>
                <Text style={styles.sectionLabel}>Incoming requests</Text>
                {incoming.length > 0 && (
                  <View style={styles.inboxBadge}>
                    <Text style={styles.inboxBadgeText}>{incoming.length}</Text>
                  </View>
                )}
              </View>
              {incoming.length === 0 ? (
                <Text style={styles.emptyText}>No pending requests.</Text>
              ) : (
                incoming.map((req) => (
                  <View key={req.id} style={styles.requestCard}>
                    <View style={styles.requestInfo}>
                      <Text style={styles.requestName}>{req.from.name}</Text>
                      <Text style={styles.requestMeta}>wants to connect</Text>
                      <TierBadge tier={req.tier} />
                    </View>
                    <View style={styles.requestActions}>
                      <Pressable
                        disabled={busy}
                        onPress={() => acceptRequest(req.from.name, req.tier, req.id)}
                        style={styles.acceptBtn}
                      >
                        <Text style={styles.acceptBtnText}>Accept</Text>
                      </Pressable>
                      <Pressable
                        disabled={busy}
                        onPress={() => declineRequest(req.from.name, req.id)}
                        style={styles.declineBtn}
                      >
                        <Text style={styles.declineBtnText}>Decline</Text>
                      </Pressable>
                    </View>
                  </View>
                ))
              )}
            </View>

            {/* Send request */}
            <View style={styles.card}>
              <Text style={styles.sectionLabel}>Send request</Text>
              <Text style={styles.fieldLabel}>To</Text>
              <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.chipRow}>
                {otherUsers.map((u) => (
                  <Pressable
                    key={u.id}
                    onPress={() => setSendTo(u.id)}
                    style={[styles.chip, sendTo === u.id && styles.chipActive]}
                  >
                    <Text style={[styles.chipText, sendTo === u.id && styles.chipTextActive]}>{u.name}</Text>
                  </Pressable>
                ))}
              </ScrollView>

              <Text style={styles.fieldLabel}>Tier</Text>
              <View style={styles.tierPicker}>
                {TIERS.map((t) => {
                  const meta = TIER_META[t];
                  const selected = sendTier === t;
                  return (
                    <Pressable
                      key={t}
                      onPress={() => setSendTier(t)}
                      style={[
                        styles.tierPickerItem,
                        { backgroundColor: meta.bg, borderColor: selected ? meta.color : 'transparent' },
                        selected && styles.tierPickerItemSelected,
                      ]}
                    >
                      <Text style={[styles.tierPickerLabel, { color: meta.color }]}>{meta.label}</Text>
                      <Text style={styles.tierPickerCap}>
                        cap {tierCap(t)?.cap ?? '—'}
                      </Text>
                    </Pressable>
                  );
                })}
              </View>

              <Pressable
                disabled={busy || !sendTo}
                onPress={sendRequest}
                style={[styles.primaryBtn, (!sendTo || busy) && styles.primaryBtnDisabled]}
              >
                <Text style={styles.primaryBtnText}>
                  {sendTo ? `Send ${TIER_META[sendTier].label} request` : 'Select a recipient'}
                </Text>
              </Pressable>
            </View>
          </>
        )}

        {loading && !capacity && !error && (
          <View style={styles.loadingBox}>
            <ActivityIndicator size="large" color="#16605c" />
            <Text style={styles.loadingText}>Loading…</Text>
          </View>
        )}
      </ScrollView>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: '#f4f6f6' },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingHorizontal: 20,
    paddingVertical: 12,
    backgroundColor: '#fff',
    borderBottomWidth: 1,
    borderBottomColor: '#e8e8e8',
  },
  appTitle: { fontSize: 22, fontWeight: '700', color: '#16605c' },
  appSubtitle: { fontSize: 14, color: '#666', marginTop: 2 },
  body: { padding: 16, gap: 12, paddingBottom: 32 },

  card: {
    backgroundColor: '#fff',
    borderRadius: 12,
    padding: 16,
    gap: 12,
    shadowColor: '#000',
    shadowOffset: { width: 0, height: 1 },
    shadowOpacity: 0.06,
    shadowRadius: 4,
    elevation: 2,
  },
  sectionLabel: {
    fontSize: 12,
    fontWeight: '700',
    textTransform: 'uppercase',
    letterSpacing: 0.8,
    color: '#888',
  },
  fieldLabel: { fontSize: 13, fontWeight: '600', color: '#555', marginTop: 4 },

  chipRow: { gap: 8, paddingVertical: 4 },
  chip: {
    paddingVertical: 8,
    paddingHorizontal: 14,
    borderRadius: 20,
    backgroundColor: '#f0f0f0',
    borderWidth: 1.5,
    borderColor: 'transparent',
  },
  chipActive: { backgroundColor: '#e1efed', borderColor: '#16605c' },
  chipText: { fontSize: 14, color: '#444' },
  chipTextActive: { color: '#16605c', fontWeight: '600' },

  budgetRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  budgetLabel: { fontSize: 15, fontWeight: '600', color: '#333' },
  budgetValue: { fontSize: 15, fontWeight: '700', color: '#16605c' },
  budgetValueFull: { color: '#9a3b2e' },
  fullHint: { fontSize: 12, color: '#9a3b2e', fontStyle: 'italic' },

  progressTrack: {
    height: 8,
    backgroundColor: '#eee',
    borderRadius: 4,
    overflow: 'hidden',
  },
  progressFill: { height: '100%', borderRadius: 4 },

  tierCapList: { gap: 10, marginTop: 4 },
  tierCapRow: { gap: 4 },
  tierCapHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  tierCapCount: { fontSize: 13, fontWeight: '600', color: '#555' },
  tierCapCountFull: { color: '#9a3b2e' },

  tierBadge: {
    paddingVertical: 4,
    paddingHorizontal: 10,
    borderRadius: 6,
    alignSelf: 'flex-start',
  },
  tierBadgeSmall: { paddingVertical: 2, paddingHorizontal: 8 },
  tierBadgeText: { fontSize: 13, fontWeight: '700' },
  tierBadgeTextSmall: { fontSize: 11 },

  tierSection: {
    borderLeftWidth: 4,
    borderRadius: 8,
    padding: 12,
    gap: 8,
    marginTop: 4,
  },
  tierSectionHeader: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center' },
  tierSectionCount: { fontSize: 12, color: '#666', fontWeight: '600' },

  contactCard: {
    backgroundColor: '#fff',
    borderRadius: 8,
    padding: 12,
    gap: 8,
    borderWidth: 1,
    borderColor: '#e8e8e8',
  },
  contactName: { fontSize: 16, fontWeight: '600', color: '#222' },
  contactActions: { gap: 6 },
  moveLabel: { fontSize: 11, color: '#888', textTransform: 'uppercase', letterSpacing: 0.5 },
  moveRow: { flexDirection: 'row', flexWrap: 'wrap', gap: 6 },
  moveBtn: {
    paddingVertical: 6,
    paddingHorizontal: 12,
    borderRadius: 6,
    borderWidth: 1.5,
    backgroundColor: '#fff',
  },
  moveBtnText: { fontSize: 13, fontWeight: '600' },
  removeBtn: {
    paddingVertical: 6,
    paddingHorizontal: 12,
    borderRadius: 6,
    backgroundColor: '#f6e7e4',
    borderWidth: 1,
    borderColor: '#e8c4bc',
  },
  removeBtnText: { fontSize: 13, fontWeight: '600', color: '#9a3b2e' },

  inboxHeader: { flexDirection: 'row', alignItems: 'center', gap: 8 },
  inboxBadge: {
    backgroundColor: '#16605c',
    borderRadius: 10,
    minWidth: 20,
    height: 20,
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: 6,
  },
  inboxBadgeText: { color: '#fff', fontSize: 11, fontWeight: '700' },

  requestCard: {
    borderWidth: 1,
    borderColor: '#e8e8e8',
    borderRadius: 10,
    padding: 14,
    gap: 12,
    backgroundColor: '#fafafa',
  },
  requestInfo: { gap: 4 },
  requestName: { fontSize: 17, fontWeight: '700', color: '#222' },
  requestMeta: { fontSize: 13, color: '#888' },
  requestActions: { flexDirection: 'row', gap: 8 },
  acceptBtn: {
    flex: 1,
    paddingVertical: 10,
    borderRadius: 8,
    backgroundColor: '#16605c',
    alignItems: 'center',
  },
  acceptBtnText: { color: '#fff', fontSize: 15, fontWeight: '600' },
  declineBtn: {
    flex: 1,
    paddingVertical: 10,
    borderRadius: 8,
    backgroundColor: '#fff',
    borderWidth: 1,
    borderColor: '#ccc',
    alignItems: 'center',
  },
  declineBtnText: { color: '#555', fontSize: 15, fontWeight: '600' },

  tierPicker: { flexDirection: 'row', gap: 8 },
  tierPickerItem: {
    flex: 1,
    padding: 12,
    borderRadius: 10,
    borderWidth: 2,
    alignItems: 'center',
    gap: 2,
  },
  tierPickerItemSelected: { borderWidth: 2 },
  tierPickerLabel: { fontSize: 14, fontWeight: '700' },
  tierPickerCap: { fontSize: 11, color: '#666' },

  primaryBtn: {
    paddingVertical: 14,
    borderRadius: 10,
    backgroundColor: '#16605c',
    alignItems: 'center',
    marginTop: 4,
  },
  primaryBtnDisabled: { opacity: 0.45 },
  primaryBtnText: { color: '#fff', fontSize: 16, fontWeight: '600' },

  emptyText: { fontSize: 14, color: '#999', lineHeight: 20 },
  emptyTextSmall: { fontSize: 13, color: '#aaa', fontStyle: 'italic' },

  bannerError: {
    backgroundColor: '#f6e7e4',
    borderRadius: 10,
    padding: 14,
    gap: 6,
    borderWidth: 1,
    borderColor: '#e8c4bc',
  },
  bannerTitle: { fontSize: 14, fontWeight: '700', color: '#9a3b2e' },
  bannerText: { fontSize: 14, color: '#9a3b2e', lineHeight: 20 },
  bannerHint: { fontSize: 12, color: '#9a3b2e', opacity: 0.85 },
  bannerBtn: {
    alignSelf: 'flex-start',
    marginTop: 4,
    paddingVertical: 8,
    paddingHorizontal: 14,
    borderRadius: 6,
    backgroundColor: '#9a3b2e',
  },
  bannerBtnText: { color: '#fff', fontSize: 13, fontWeight: '600' },
  bannerBtnOutline: {
    alignSelf: 'flex-start',
    marginTop: 4,
    paddingVertical: 8,
    paddingHorizontal: 14,
    borderRadius: 6,
    borderWidth: 1,
    borderColor: '#9a3b2e',
  },
  bannerBtnOutlineText: { color: '#9a3b2e', fontSize: 13, fontWeight: '600' },

  bannerSuccess: {
    backgroundColor: '#e8f5e9',
    borderRadius: 10,
    padding: 14,
    borderWidth: 1,
    borderColor: '#a5d6a7',
  },
  bannerSuccessText: { fontSize: 14, color: '#2e7d32', fontWeight: '600' },

  loadingBox: { alignItems: 'center', paddingVertical: 48, gap: 12 },
  loadingText: { fontSize: 14, color: '#888' },
});
