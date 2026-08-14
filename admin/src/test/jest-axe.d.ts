declare module "jest-axe" {
  export interface AxeResults {
    violations: unknown[];
  }

  export function axe(node: Element): Promise<AxeResults>;
}
