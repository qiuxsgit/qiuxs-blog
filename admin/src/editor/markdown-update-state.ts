export interface MarkdownUpdateState {
  readonly exactPasteDocument?: string;
  readonly stopped: boolean;
}

export interface MarkdownUpdateDecision {
  readonly state: MarkdownUpdateState;
  readonly markdown?: string;
}

export const initialMarkdownUpdateState: MarkdownUpdateState = { stopped: false };

export function markExactPasteDocument(
  state: MarkdownUpdateState,
  serializedMarkdown: string,
): MarkdownUpdateState {
  return state.stopped ? state : { ...state, exactPasteDocument: serializedMarkdown };
}

export function stopMarkdownUpdates(): MarkdownUpdateState {
  return { stopped: true };
}

export function reconcileMarkdownUpdate(
  state: MarkdownUpdateState,
  callbackMarkdown: string,
  currentMarkdown: string,
): MarkdownUpdateDecision {
  if (state.stopped || callbackMarkdown !== currentMarkdown) return { state };
  if (state.exactPasteDocument !== undefined) {
    const nextState = initialMarkdownUpdateState;
    if (callbackMarkdown === state.exactPasteDocument) return { state: nextState };
    return { state: nextState, markdown: callbackMarkdown };
  }
  return { state, markdown: callbackMarkdown };
}
