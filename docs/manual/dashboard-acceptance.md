---
type: Guide
title: Web Dashboard Manual Acceptance Checklist
description: Comprehensive manual acceptance test checklist and operational verification guide for the Relay embedded web dashboard interface.
status: stable
---

# Web Dashboard Manual Acceptance Checklist

This document provides the definitive manual acceptance test checklist for verifying the Relay embedded web dashboard. Operators and developers can use this guide to validate each tab, view, control action, and administrative feature in the dashboard interface.

## Prerequisites

1. Start a local Relay control plane instance with sample application connectivity.
2. Open a web browser and navigate to the Relay dashboard URL (default: `http://localhost:8080/` or the configured listener address).
3. Ensure authentication credentials (API key or OIDC bearer token) are available if authentication is enabled on the control plane.

---

## Navigation & Global Shell

- [ ] **Sidebar Navigation**: Verify that the sidebar contains links for Fleet & Apps, Workflows, Queues, Schedules, Alert Rules, and API Keys. Clicking each navigation item switches the active view route without full-page reloads.
- [ ] **Brand Identity**: Confirm the Relay branding and version badge render correctly in the sidebar header.
- [ ] **Top Header & Application Selector**: Verify the application dropdown selector correctly lists registered applications. Switching applications updates the active application context across all dashboard views.
- [ ] **Refresh Control**: Clicking the refresh button reloads data for the current active view.
- [ ] **Theme Toggle**: Clicking the theme toggle button switches between light and dark themes instantly, persisting the preference in browser storage.
- [ ] **Authentication State**: If the instance requires authentication, verify that unauthorized requests prompt for an API key or bearer token via sign-in modals and inline login cards.

---

## 1. Fleet & Applications Tab

- [ ] **Fleet Metrics Summary**: Verify stat cards display total registered applications, connected healthy executors, and disconnected executors.
- [ ] **Connected Executors Table**:
  - Verify columns display Executor ID, Status pill (`HEALTHY`, `DISCONNECTED`), Hostname, App Version, Language runtime, and Last Seen timestamp.
  - Confirm status pills use distinct visual styling for healthy versus disconnected executors.
- [ ] **Registered Applications Table**:
  - Verify listing of all registered applications in the organization.
  - Confirm columns show Application Name, Status, Executor Timeout, and Private Mode configuration.
  - Clicking an application row updates the active application context.

---

## 2. Workflows List & Filtering Tab

- [ ] **Workflow Search & Filtering**:
  - **Text Search**: Enter workflow ID search queries in the search input to filter the workflow table in real-time.
  - **Status Filter**: Select workflow status filters (`SUCCESS`, `PENDING`, `ERROR`, `CANCELLED`, `ENQUEUED`) from the dropdown and confirm the table correctly filters rows.
- [ ] **Workflow Table Columns**:
  - Verify table displays Workflow ID, Status pill, Workflow Name, Queue Name, App Version, Created At timestamp, and calculated execution Duration.
- [ ] **Workflow Detail Navigation**: Clicking any workflow row navigates to the dedicated workflow detail view.

---

## 3. Workflow Detail & Steps Timeline

- [ ] **Workflow Metadata Header**:
  - Verify header displays Workflow ID, status pill, workflow name, assigned queue, creation timestamp, and total duration.
  - **Workflow Control Actions**:
    - For pending or enqueued workflows, verify the **Cancel** button functions correctly and updates workflow state.
    - For cancelled workflows, verify the **Resume** button restores execution.
    - For failed workflows, verify the **Restart** button forks a new workflow execution and redirects to the new execution ID.
- [ ] **Workflow Family DAG & Canvas Viewport**:
  - Verify the bespoke SVG DAG renderer graphs execution steps and child workflow branches in dedicated container cards.
  - Confirm cubic bezier execution curves link parent steps to child workflow nodes with outcome-colored strokes (`SUCCESS`, `ERROR`, `PENDING`).
  - Verify the canvas viewport controls (`+`, `-`, `Reset`), mouse wheel zooming, and drag-to-pan across the dot-grid background.
  - Confirm step nodes display step names, sequence IDs, and status indicator dots.
- [ ] **Step Inspection Drawer & Modal**:
  - Clicking any step node in the DAG opens the step inspection drawer or modal.
  - Verify drawer displays step ID, name, status, start and completion timestamps, and JSON viewer for output/result or error stacks.
  - If a step spawned child workflows, verify child cards display quick-navigation action pills.
- [ ] **Live Telemetry & Hotkeys**:
  - Verify the polling selector allows switching between Off, 2s, 5s, and 15s intervals with an active status indicator dot.
  - Verify keyboard navigation: press `?` to open the hotkey cheat sheet and `/` to focus search filters.
- [ ] **Workflow Tabs (Inputs/Outputs, Events, Notifications)**:
  - **Inputs & Outputs Tab**: Verify JSON viewer renders workflow input payloads and output results (or error details).
  - **Events Tab**: Verify table lists workflow synchronization event keys and JSON values.
  - **Notifications Tab**: Verify table lists notification topics, messages, consumption status, and timestamps.

---

## 4. Queues Tab

- [ ] **Queue Listing**:
  - Verify table displays Queue Name, Concurrency limit, Worker Concurrency, Rate Limit configuration, Priority status, and Partition Queue status.
  - Confirm unlimited concurrency is clearly indicated when null.
  - Verify rate limits display maximum requests per time period.

---

## 5. Schedules Tab

- [ ] **Scheduled Jobs Listing**:
  - Verify table displays Schedule Name, target Workflow Name, Cron Expression, status pill (`ACTIVE`, `PAUSED`), Last Fired timestamp, and action controls.
- [ ] **Schedule Actions**:
  - **Pause / Resume**: Click pause on an active schedule and verify status updates to paused. Click resume to reactivate.
  - **Trigger Now**: Click "Trigger Now" and verify it immediately invokes the scheduled workflow, displaying the new workflow ID and navigating to its detail view.

---

## 6. Alerting Rules Tab

- [ ] **Alert Rules Listing**:
  - Verify table displays Rule ID, Rule Type (`WorkflowFailure`, `SlowQueue`, `UnresponsiveApplication`), Minimum Interval in seconds, rule metadata JSON, and Last Fired timestamp.
- [ ] **Rule Management**:
  - **Create Rule**: Click "+ New Alert Rule", fill in rule type, interval, and metadata JSON in the modal, and submit. Verify the new rule appears in the table.
  - **Delete Rule**: Click delete on an alert rule, confirm the deletion prompt, and verify the rule is removed from the list.

---

## 7. API Keys & Authentication Tab

- [ ] **API Keys Listing**:
  - Verify table displays Key Name, Permission scopes, Scoped Applications, and Creation timestamp.
- [ ] **Minting Scoped API Keys**:
  - Click "+ Mint API Key", provide a key name, select an application scope (or all applications), and submit.
  - Verify the generated secret key token is presented in a modal with copy functionality. Confirm warning notice that the token is only shown once.
- [ ] **Revoking API Keys**:
  - Click revoke on an API key, confirm the revocation prompt, and verify the key is removed from the active list.

---

## 8. Theme Toggle & Accessibility

- [ ] **Theme Persistence**: Verify toggling between dark and light themes updates UI colors across all screens and correctly persists selection across browser sessions via `localStorage`.
- [ ] **Responsive Layout**: Verify sidebar and content areas resize gracefully across standard desktop viewports.
