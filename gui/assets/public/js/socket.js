var quotaSort;

document.addEventListener("DOMContentLoaded", function () {
    quotaSort = new Tablesort(document.getElementById('work-quota-table'), {
        descending: true
    });

    ws = new WebSocket('wss://' + window.location.host + '/ws');
    ws.addEventListener('message', function (e) {
        var msg = JSON.parse(e.data);
        updateElement("pool-hash-rate", msg.poolhashrate);
        updateElement("last-work-height", msg.lastworkheight);
        updateElement("last-payment-height", msg.lastpaymentheight);
        if (msg.workquotas == null) {
            msg.workquotas = [];
        }
        updateWorkQuotas(msg.workquotas);
    });
});

function updateElement(elementID, value) {
    el = document.getElementById(elementID);
    if (el.innerHTML != value) {
        el.innerHTML = value;
        flashElement(el)
    }
}

function flashElement(el) {
    el.classList.add("flash");
    setTimeout(function () { el.classList.remove("flash"); }, 1000);
}

function updateWorkQuotas(quotas) {
    // Ensure all received quotas are in the table
    var changeMade = false;
    for (i = 0; i < quotas.length; i++) {
        var exists = false;
        var rows = document.getElementById('work-quota-table').querySelector('tbody').querySelectorAll('tr');
        for (j = 0; j < rows.length; j++) {
            var accountID = rows[j].querySelector('.quota-accountid');
            if (quotas[i].accountid == accountID.innerHTML) {
                // Row already exists for this account ID
                exists = true;
                // Update percentage for this account if required
                var percent = rows[j].querySelector('.quota-percent');
                if (quotas[i].percent != percent.innerHTML) {
                    percent.innerHTML = quotas[i].percent;
                    flashElement(percent);
                    changeMade = true;
                }
            }
        }

        if (exists == false) {
            // Add a new row for this account
            var newRow = document.getElementById('work-quota-table').querySelector('tbody').insertRow(0);
            var cell1 = newRow.insertCell(0);
            cell1.innerHTML = quotas[i].accountid;
            cell1.classList.add("quota-accountid");
            var cell2 = newRow.insertCell(1);
            cell2.innerHTML = quotas[i].percent;
            cell2.classList.add("quota-percent");
            flashElement(newRow);
            changeMade = true;
        }
    }

    // Find any rows which are no longer included in quotas
    var rowsToRemove = [];
    var rows = document.getElementById('work-quota-table').querySelector('tbody').querySelectorAll('tr');
    for (j = 0; j < rows.length; j++) {
        var exists = false;
        for (i = 0; i < quotas.length; i++) {
            var accountID = rows[j].querySelector('.quota-accountid');
            if (accountID.innerHTML == quotas[i].accountid) {
                exists = true
                break
            }
        }
        if (exists == false) {
            rowsToRemove.push(rows[j]);
        }
    }
    
    // Remove the unnecessary rows
    for (j = 0; j < rowsToRemove.length; j++) {
        rowsToRemove[j].parentNode.removeChild(rowsToRemove[j]);
        changeMade = true;
    }

    if (changeMade) {
        quotaSort.refresh();
    }
}
