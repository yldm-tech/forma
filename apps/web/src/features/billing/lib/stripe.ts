import { type Stripe, loadStripe } from "@stripe/stripe-js";

// Stripe.js is loaded on first use and kept for the page's lifetime: one publishable key per deployment.
let stripePromise: Promise<Stripe | null> | null = null;

const stripeJs = (publishableKey: string): Promise<Stripe | null> =>
  (stripePromise ??= loadStripe(publishableKey));

export interface ScaOutcome {
  status: "succeeded" | "processing" | "failed";
  paymentIntentId: string | null;
}

/** forma2's confirmUpgradeSca: on-session 3D Secure for an upgrade invoice's PaymentIntent, with the saved default card. Without a publishable key nothing can be confirmed. */
export const confirmUpgradePayment = async (
  publishableKey: string | null,
  clientSecret: string
): Promise<ScaOutcome> => {
  if (!publishableKey) return { status: "failed", paymentIntentId: null };
  const stripe = await stripeJs(publishableKey);
  if (!stripe) return { status: "failed", paymentIntentId: null };
  const { paymentIntent, error } = await stripe.confirmCardPayment(clientSecret);
  if (error) return { status: "failed", paymentIntentId: error.payment_intent?.id ?? null };
  if (paymentIntent?.status === "succeeded")
    return { status: "succeeded", paymentIntentId: paymentIntent.id };
  if (paymentIntent?.status === "processing")
    return { status: "processing", paymentIntentId: paymentIntent.id };
  return { status: "failed", paymentIntentId: paymentIntent?.id ?? null };
};
