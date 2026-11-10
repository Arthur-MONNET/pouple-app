/** The connectors that have settings of their own. They are set under Settings, in the menu of the connectors. */
const WITH_SETTINGS: ReadonlySet<string> = new Set(['gitlab']);

export const hasSettings = (connectorId: string): boolean => WITH_SETTINGS.has(connectorId);

/** The address of a connector's settings: Settings opens on the category of that name. */
export const connectorSettingsLocation = (connectorId: string): { path: string; hash: string } => ({ path: '/settings', hash: `#${connectorId}` });
