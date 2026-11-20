import Stripe from 'stripe';
const stripe = new Stripe(process.env.STRIPE_KEY, {apiVersion: '2025-03-31.basil'});
export const preview = () => stripe.invoices.createPreview({customer: 'customer'});
