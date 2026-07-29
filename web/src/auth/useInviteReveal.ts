// The mint-then-reveal state machine behind InviteReveal.vue (LYCM-105).
//
// Two screens drive the same flow — Household hands keys to housemates, Settings
// hands you one for your next device — and the fiddly part is identical in both:
// a plaintext secret that exists only in memory, a dismissal that may or may not
// have lost it, and a recovery path that has to know who the key was for. That is
// too much to keep in step by hand across two views, so it lives here.
//
// The mint call is the only thing that differs, so it is the only thing passed in.

import { computed, ref } from 'vue'
import { reinviteMember, requestDeviceInvite, type Invite } from '@/api/auth'
import { useAuthStore } from '@/stores/auth'

/** A key that was dismissed uncopied, and enough to offer another. */
interface Lost {
  name: string
  userId: number
  /** Whether it was your own device key — the copy differs throughout. */
  self: boolean
}

export function useInviteReveal(onMinted?: () => void | Promise<void>) {
  const auth = useAuthStore()

  const invite = ref<Invite | null>(null)
  const lost = ref<Lost | null>(null)
  /** True while a mint is in flight, so a slow LAN can't be double-clicked. */
  const minting = ref(false)
  const reissuing = ref(false)
  const error = ref<string | null>(null)

  /**
   * Whether the revealed key is your own.
   *
   * "Hand this to Mara" is the right thing to say about a housemate's key and
   * nonsense about your own, so every string in the reveal forks on this.
   */
  const self = computed(() => !!invite.value && invite.value.user.id === auth.user?.id)

  /** Show a key that has already been minted (the create-a-housemate path). */
  function present(minted: Invite): void {
    invite.value = minted
    lost.value = null
  }

  async function mint(call: () => Promise<Invite>, busy = minting): Promise<void> {
    if (busy.value) return
    busy.value = true
    error.value = null
    try {
      present(await call())
      await onMinted?.()
    } catch (err) {
      error.value = err instanceof Error ? err.message : 'could not issue a key'
    } finally {
      busy.value = false
    }
  }

  /** "Add a device" — a key for the signed-in person. */
  function addDevice(): Promise<void> {
    return mint(requestDeviceInvite)
  }

  /** A fresh key for a housemate: a second device, or one they never redeemed. */
  function reinvite(userId: number): Promise<void> {
    return mint(() => reinviteMember(userId))
  }

  /**
   * Closing the reveal is the point of no return: the plaintext exists only here,
   * and the server kept nothing but its hash.
   *
   * If they copied it (or said they had it), just close. If they walked away from
   * it, hand over to the "that invite is gone" state — honest about what happened,
   * and offering the only real fix. Telling someone who *just copied the key* that
   * it is gone would be both wrong and alarming.
   */
  function close(saved: boolean): void {
    const shown = invite.value
    const wasSelf = self.value
    invite.value = null
    lost.value =
      saved || !shown
        ? null
        : { name: shown.user.display_name, userId: shown.user.id, self: wasSelf }
  }

  /** Re-issue after a reveal was dismissed uncopied — down the path it came from. */
  function reissue(): Promise<void> {
    const gone = lost.value
    if (!gone) return Promise.resolve()
    return mint(gone.self ? requestDeviceInvite : () => reinviteMember(gone.userId), reissuing)
  }

  return {
    invite,
    lost,
    minting,
    reissuing,
    error,
    self,
    present,
    addDevice,
    reinvite,
    close,
    reissue,
  }
}
