// confirmModal.js
import { appConfig } from "./config.js";

const confirmModal = document.getElementById("confirm-modal");
const confirmOk = document.getElementById("confirm-ok");
const confirmCancel = document.getElementById("confirm-cancel");
let _confirmResolve = null;
let _confirmTimer = null;

export function showConfirmModal() {
  return new Promise((resolve) => {
    if (!confirmModal) return resolve(true);
    confirmModal.classList.remove("confirm-hidden");
    confirmModal.setAttribute("aria-hidden", "false");
    _confirmResolve = resolve;
    confirmOk.onclick = () => {
      closeConfirm(true);
    };
    confirmCancel.onclick = () => {
      closeConfirm(false);
    };
    if (appConfig.CONFIRM_AUTO_CANCEL_SEC > 0) {
      _confirmTimer = setTimeout(
        () => closeConfirm(false),
        appConfig.CONFIRM_AUTO_CANCEL_SEC * 1000
      );
    }
  });
}

export function closeConfirm(result) {
  if (!confirmModal) return;
  confirmModal.classList.add("confirm-hidden");
  confirmModal.setAttribute("aria-hidden", "true");
  if (_confirmTimer) {
    clearTimeout(_confirmTimer);
    _confirmTimer = null;
  }
  if (typeof _confirmResolve === "function") {
    _confirmResolve(result);
    _confirmResolve = null;
  }
}
