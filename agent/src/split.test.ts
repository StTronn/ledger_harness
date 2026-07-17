// split.test.ts — pins the two §8 test vectors the prompt mandates. Run with
// `npm test`; it exits non-zero on any mismatch so CI (and a human) can trust the
// agent's arithmetic agrees with the Go canonical split without an API key.
import { split } from "./split.ts";

let failures = 0;

function expectSplit(gross: number, rate: number, want: { net: number; gst: number }): void {
  const got = split(gross, rate);
  const ok = got.net === want.net && got.gst === want.gst;
  const label = `split(${gross}, ${rate})`;
  if (ok) {
    console.log(`ok   ${label} => {net:${got.net}, gst:${got.gst}}`);
  } else {
    failures++;
    console.error(
      `FAIL ${label} => {net:${got.net}, gst:${got.gst}}, want {net:${want.net}, gst:${want.gst}}`,
    );
  }
}

// The mandated vectors (both at 18%): a dtc_sale gross and a refund gross.
expectSplit(265878, 18, { net: 225320, gst: 40558 });
expectSplit(248591, 18, { net: 210670, gst: 37921 });

// Invariant: net + gst === gross to the paise (the split must be exact).
for (const [gross, rate] of [
  [265878, 18],
  [248591, 18],
  [386158, 5],
] as const) {
  const { net, gst } = split(gross, rate);
  if (net + gst !== gross) {
    failures++;
    console.error(`FAIL exactness: split(${gross}, ${rate}) net+gst=${net + gst} != ${gross}`);
  }
}

if (failures > 0) {
  console.error(`\n${failures} test(s) failed`);
  process.exit(1);
}
console.log("\nall split vectors passed");
