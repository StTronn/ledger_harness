// tools.ts is the agent's READ-ONLY tool layer over the snapshotted Razorpay
// fixtures (worlds/<world>/<period>/razorpay/*). These are the only data the agent
// can reach — the committed snapshot, NOT live Razorpay and NEVER truth/. Each tool
// returns the object plus enough to cite it (the object id + the field path).
//
// The tools load each file once and index by id, so a batch run is O(1) per lookup.

import { join } from "node:path";
import { readJSON, razorpayDir } from "./io.ts";

interface RawNotes {
  sku?: string;
  gst_rate?: string;
}
interface RawOrder {
  id: string;
  amount: number;
  notes: RawNotes;
}
interface RawPayment {
  id: string;
  amount: number;
  order_id: string;
  notes: RawNotes;
}
interface RawRefund {
  id: string;
  amount: number;
  payment_id: string;
  notes: RawNotes;
}
interface RawSettlement {
  id: string;
  amount: number;
  fee: number;
  tax: number;
  payment_ids: string[];
  refund_ids: string[];
  utr: string;
}

// Tools is the read-only snapshot accessor bundle handed to every brain. Each getter
// returns undefined when the id is absent (the brain decides to escalate).
export interface Tools {
  getOrder(id: string): RawOrder | undefined;
  getPayment(id: string): RawPayment | undefined;
  getRefund(id: string): RawRefund | undefined;
  getSettlement(id: string): RawSettlement | undefined;
}

function indexByID<T extends { id: string }>(rows: T[]): Map<string, T> {
  const m = new Map<string, T>();
  for (const r of rows) if (!m.has(r.id)) m.set(r.id, r);
  return m;
}

// loadTools reads the period's snapshot files and builds the indexed tool bundle.
// Missing files (e.g. no disputes) are tolerated as empty.
export function loadTools(root: string, world: string, period: string): Tools {
  const dir = razorpayDir(root, world, period);
  const load = <T extends { id: string }>(name: string): Map<string, T> => {
    try {
      return indexByID(readJSON<T[]>(join(dir, name)));
    } catch {
      return new Map<string, T>();
    }
  };
  const orders = load<RawOrder>("orders.json");
  const payments = load<RawPayment>("payments.json");
  const refunds = load<RawRefund>("refunds.json");
  const settlements = load<RawSettlement>("settlements.json");
  return {
    getOrder: (id) => orders.get(id),
    getPayment: (id) => payments.get(id),
    getRefund: (id) => refunds.get(id),
    getSettlement: (id) => settlements.get(id),
  };
}

export type { RawOrder, RawPayment, RawRefund, RawSettlement };
