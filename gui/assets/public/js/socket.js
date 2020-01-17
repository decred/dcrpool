var quotaSort;
var minedBlocksSort;

document.addEventListener("DOMContentLoaded", function () {
    quotaSort = new Tablesort(document.getElementById('work-quota-table'), {
        descending: true
    });

    minedBlocksSort = new Tablesort(document.getElementById('mined-blocks-table'), {
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
        if (msg.minedblocks == null) {
            msg.minedblocks = [];
        }
        updateMinedBlocks(msg.minedblocks);
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
function removeElement(el) {
    el.parentNode.removeChild(el);
}

function updateMinedBlocks(minedBlocks) {
    blocksTableBody = document.getElementById('mined-blocks-table').querySelector('tbody');

    // Ensure all received blocks are in the table
    var changeMade = false;
    for (i = 0; i < minedBlocks.length; i++) {
        var exists = false;
        var rows = blocksTableBody.querySelectorAll('tr');
        for (j = 0; j < rows.length; j++) {
            var blockHeight = rows[j].getAttribute('data-row-id');
            if (minedBlocks[i].blockheight == blockHeight) {
                exists = true;
                break;
            }
        }

        if (exists == false) {
            // Add a new row for this block
            var newRow = blocksTableBody.insertRow(0);
            newRow.setAttribute("data-row-id", minedBlocks[i].blockheight);
            var a = document.createElement('a');
            a.setAttribute('href', minedBlocks[i].blockurl);
            a.innerHTML = minedBlocks[i].blockheight;
            newRow.insertCell(0).appendChild(a);
            newRow.insertCell(1).innerHTML = minedBlocks[i].miner;
            var span = document.createElement('span');
            span.innerHTML = minedBlocks[i].minedby;
            span.className = "dcr-label";
            newRow.insertCell(2).appendChild(span); 
            flashElement(newRow);
            changeMade = true;
        }
    }

    if (changeMade) {
        minedBlocksSort.refresh();
    }

    // Remove all but the 10 most recent blocks
    var rowsToRemove = blocksTableBody.querySelectorAll('tr:nth-child(10) ~ tr');
    for (j = 0; j < rowsToRemove.length; j++) {
        removeElement(rowsToRemove[j]);
    }
}

function updateWorkQuotas(quotas) {
    quotasTableBody = document.getElementById('work-quota-table').querySelector('tbody');

    // Ensure all received quotas are in the table
    var changeMade = false;
    for (i = 0; i < quotas.length; i++) {
        var exists = false;
        var rows = quotasTableBody.querySelectorAll('tr');
        for (j = 0; j < rows.length; j++) {
            var accountID = rows[j].getAttribute('data-row-id');
            if (quotas[i].accountid == accountID) {
                // Row already exists for this account ID
                exists = true;
                // Update percentage for this account if required
                var percent = rows[j].cells[0];
                if (quotas[i].percent != percent.innerHTML) {
                    percent.innerHTML = quotas[i].percent;
                    flashElement(percent);
                    changeMade = true;
                }
                break;
            }
        }

        if (exists == false) {
            // Add a new row for this account
            var newRow = quotasTableBody.insertRow(0);
            newRow.setAttribute("data-row-id", quotas[i].accountid);
            newRow.insertCell(0).innerHTML = quotas[i].percent;
            var span = document.createElement('span');
            span.innerHTML = quotas[i].accountid;
            span.className = "dcr-label";
            newRow.insertCell(1).append(span);
            flashElement(newRow);
            changeMade = true;
        }
    }

    // Find any rows which are no longer included in quotas
    var rowsToRemove = [];
    var rows = quotasTableBody.querySelectorAll('tr');
    for (j = 0; j < rows.length; j++) {
        var exists = false;
        for (i = 0; i < quotas.length; i++) {
            if (rows[j].getAttribute('data-row-id') == quotas[i].accountid) {
                exists = true;
                break;
            }
        }
        if (exists == false) {
            rowsToRemove.push(rows[j]);
        }
    }
    
    // Remove the unnecessary rows
    for (j = 0; j < rowsToRemove.length; j++) {
        removeElement(rowsToRemove[j]);
        changeMade = true;
    }

    if (changeMade) {
        quotaSort.refresh();
    }
}
