import type { Account, ColumnNode } from "./types";

/** Canonical display order of account groups. */
const GROUP_ORDER: string[] = ["assets", "liabilities", "income", "expense"];

function groupLabel(group: string): string {
  return group.charAt(0).toUpperCase() + group.slice(1);
}

/**
 * Build a two-level column tree: group nodes (in canonical order:
 * assets, liabilities, income, expense; any unknown groups appended in
 * first-seen order) whose children are the leaf account columns,
 * preserving the original account order within each group.
 */
export function buildColumns(accounts: Account[]): ColumnNode[] {
  const byGroup = new Map<string, Account[]>();
  for (const account of accounts) {
    const list = byGroup.get(account.group);
    if (list) {
      list.push(account);
    } else {
      byGroup.set(account.group, [account]);
    }
  }

  const orderedGroups: string[] = [];
  for (const group of GROUP_ORDER) {
    if (byGroup.has(group)) orderedGroups.push(group);
  }
  for (const group of byGroup.keys()) {
    if (!orderedGroups.includes(group)) orderedGroups.push(group);
  }

  return orderedGroups.map((group) => {
    const groupAccounts = byGroup.get(group) ?? [];
    return {
      key: group,
      label: groupLabel(group),
      children: groupAccounts.map((account) => ({
        key: account.path,
        label: account.label,
        account: account.path,
      })),
    };
  });
}
