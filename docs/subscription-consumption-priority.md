# Subscription consumption priority

Users can reorder their active packages in **Console → Top up → My subscriptions**.
Move a package up or down and save the order. Restoring the default uses the
package with the earliest expiry first, then the lowest subscription ID.

The order applies when billing uses subscription quota. The existing
subscription/wallet preference still controls which funding source is tried.
If a package cannot cover a request's reservation, billing tries the next
eligible package. New or unranked packages follow the saved choices in default
order. Expired and cancelled packages cannot be used, even if an older saved
preference still mentions them.

`GET /api/subscription/self` includes `subscription_priority` and returns active
`subscriptions` in consumption order. To save an order:

```http
PUT /api/subscription/self/priority
Content-Type: application/json

{"subscription_ids":[123,124]}
```

The IDs identify the user's purchased subscriptions, not plan catalogue IDs.
The authenticated user owns the preference; request-supplied user IDs are
ignored. Every submitted ID must be unique, positive, active, unexpired and
owned by that user. A missing or null field is rejected. An empty array
explicitly restores the default. At most 1,000 IDs can be submitted.

The additive `subscription_consumption_preferences` table stores the preference
separately from quota snapshots. Updating the order changes no subscription
quota, expiry, reset time, group, wallet preference or transaction. In-flight
charges, resets and refunds cannot overwrite it. Existing request receipts
remain bound to the original selected package after a preference change.

Deployments of the shared OpenCodex NewAPI use the canonical `grey0758/new-api`
`main` source and signed immutable production images. Per-site configuration
remains local. The independent tt-api fork is excluded from this rollout.
