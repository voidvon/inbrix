import { getVapidPublicKey, removePushSubscription, savePushSubscription } from "./api";

export function supportsWebPush() {
  return "serviceWorker" in navigator && "PushManager" in window && "Notification" in window;
}

export async function currentPushSubscription() {
  if (!supportsWebPush()) return null;
  const registration = await navigator.serviceWorker.getRegistration("/");
  if (!registration) return null;
  return registration.pushManager.getSubscription();
}

export async function enableWebPush(locale: string) {
  if (!supportsWebPush()) throw new Error("Web Push is not supported by this browser");
  const permission = await Notification.requestPermission();
  if (permission !== "granted") throw new Error("Notification permission was not granted");

  const registration = await navigator.serviceWorker.register(`/sw.js?locale=${encodeURIComponent(locale)}`, { scope: "/" });
  const existing = await registration.pushManager.getSubscription();
  const subscription = existing ?? await getVapidPublicKey().then(({ publicKey }) => registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: decodeBase64URL(publicKey),
  }));
  if (!subscription) throw new Error("Push subscription could not be created");
  await savePushSubscription(subscription.toJSON());
  return subscription;
}

export async function disableWebPush() {
  const subscription = await currentPushSubscription();
  if (!subscription) return;
  await removePushSubscription(subscription.endpoint);
  await subscription.unsubscribe();
}

function decodeBase64URL(value: string) {
  const padding = "=".repeat((4 - value.length % 4) % 4);
  const binary = window.atob((value + padding).replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from(binary, (character) => character.charCodeAt(0));
}
