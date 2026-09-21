import { X } from "@phosphor-icons/react";
import { Modal } from "./Modal.jsx";
import { SettingsCenter } from "./SettingsCenter.jsx";
import { CredentialCenter } from "./ManagementPages.jsx";
import { ReleasePolicyEditor } from "./ReleasePolicy.jsx";
import { WebhookSettings } from "./DeploymentCenter.jsx";
import { EndpointIntegration } from "./EndpointIntegration.jsx";
import { ErrorNotice } from "./ManagementUI.jsx";

const labels = {
  model_connections: "modelConnections", local_connections: "localConnections",
  system_models: "systemModels", preferences: "preferences", api_keys: "credentials",
  webhooks: "webhooks", release_policy: "releasePolicy", endpoint_integration: "integration",
};

/** Localized labels for persisted configuration actions; no free-form model HTML. */
export function configurationLabel(section, t) {
  return t(labels[section] || "settings");
}

/** Reopens current configuration from a durable chat action; secrets never become chat data. */
export function ConfigurationPanel({ request, bootstrap, settingsProps, locale, t, onClose }) {
  const deployment = bootstrap.deployments.find(item => item.id === request.deploymentId);
  const label = configurationLabel(request.section, t);
  if (request.section === "endpoint_integration" && deployment) return <EndpointIntegration deployment={deployment} deployments={bootstrap.deployments} locale={locale} t={t} onClose={onClose}/>;
  return <Modal label={label} className="configuration-backdrop" onClose={onClose}>
    <section className="configuration-panel" onMouseDown={event => event.stopPropagation()}>
      <header><div><h2>{label}</h2>{deployment && <span>{deployment.name}</span>}</div><button type="button" onClick={onClose} title={t("close")} aria-label={t("close")}><X size={19}/></button></header>
      <div className="configuration-body">
        {["model_connections", "local_connections", "system_models", "preferences"].includes(request.section) && <SettingsCenter {...settingsProps} section={request.section} initialConnectionId={request.connectionId} />}
        {request.section === "api_keys" && <CredentialCenter embedded tenantId={bootstrap.tenantId} deployments={bootstrap.deployments} locale={locale} t={t}/>}
        {["webhooks", "release_policy", "endpoint_integration"].includes(request.section) && !deployment && <ErrorNotice error={t("noMatch")}/>}
        {deployment && request.section === "release_policy" && <ReleasePolicyEditor embedded deployment={deployment} datasets={bootstrap.datasets} t={t}/>}
        {deployment && request.section === "webhooks" && <WebhookSettings deployment={deployment} t={t}/>}
      </div>
    </section>
  </Modal>;
}
