$.fn["pagination"].defaults.locator = "data";
$.fn["pagination"].defaults.totalNumberLocator = function(response) { return response.count; };
$.fn["pagination"].defaults.nextText = '<div class="pagination-arrow pagination-arrow-right"></div>';
$.fn["pagination"].defaults.prevText = '<div class="pagination-arrow pagination-arrow-left"></div>';
$.fn["pagination"].defaults.hideWhenLessThanOnePage = true;

if ( $('#blocks-page-select').length ) {
    $('#blocks-page-select').pagination({
        dataSource: "/blocks",
        callback: function(data) {
            var html = '';
                if (data.length > 0) {
                $.each(data, function(_, item){
                    html += '<tr><td><a href="' + item.blockurl + '" rel="noopener noreferrer">' + item.blockheight + '</a></td><td>' + item.miner + '</td><td><span class="dcr-label">' + item.minedby + '</span></td></tr>';
                });
            } else {
                html += '<tr><td colspan="100%"><span class="no-data">No mined blocks</span></td></tr>';
            }
            $('#blocks-table').html(html);
        }
    });
};

if ( $('#blocks-by-account-page-select').length ) {
    $('#blocks-by-account-page-select').pagination({
        dataSource: "/account/" + accountID + "/blocks",
        callback: function(data) {
            var html = '';
            if (data.length > 0) {
                $.each(data, function(_, item){
                    html += '<tr><td><a href="' + item.blockurl + '" rel="noopener noreferrer">' + item.blockheight + '</a></td><td>' + item.confirmed + '</td><td>' + item.miner + '</td></tr>';
                });
            } else {
                html += '<tr><td colspan="100%"><span class="no-data">No mined blocks</span></td></tr>';
            }
            $('#blocks-by-account-table').html(html);
        }
    });
};

if ( $('#account-clients-page-select').length ) {
    $('#account-clients-page-select').pagination({
        dataSource: "/account/" + accountID + "/clients",
        callback: function(data) {
            var html = '';
            if (data.length > 0) {
                $.each(data, function(_, item){
                    html += '<tr><td>' + item.miner + '</td><td>' + item.hashrate + '</td></tr>';
                });
            } else {
                html += '<tr><td colspan="100%"><span class="no-data">No connected clients</span></td></tr>';
            }
            $('#account-clients-table').html(html);
        }
    });
};

if ($('#reward-quotas-page-select').length) {
    $('#reward-quotas-page-select').pagination({
        dataSource: "/rewardquotas",
        callback: function (data) {
            var html = '';
            if (data.length > 0) {
                $.each(data, function (_, item) {
                    html += '<tr><td>' + item.percent + '</td><td><span class="dcr-label">' + item.accountid + '</span></td></tr>';
                });
            } else {
                html += '<tr><td colspan="100%"><span class="no-data">No reward payments due</span></td></tr>';
            }
            $('#reward-quotas-table').html(html);
        }
    });
};

if ($('#pending-payments-page-select').length) {
    $('#pending-payments-page-select').pagination({
        dataSource: "/account/" + accountID + "/payments/pending",
        callback: function (data) {
            var html = '';
            if (data.length > 0) {
                $.each(data, function (_, item) {
                    html += '<tr><td><a href="' +  item.workheighturl + '" rel="noopener noreferrer">' +  item.workheight + '</a></td><td>' +  item.estimatedpaymentheight + '</td><td>' +  item.amount + '</td></tr>';
                });
            } else {
                html += '<tr><td colspan="100%"><span class="no-data">No pending payments</span></td></tr>';
            }
            $('#pending-payments-table').html(html);
        }
    });
};

if ($('#archived-payments-page-select').length) {
    $('#archived-payments-page-select').pagination({
        dataSource: "/account/" + accountID + "/payments/archived",
        callback: function (data) {
            var html = '';
            if (data.length > 0) {
                $.each(data, function (_, item) {
                    html += '<tr><td><a href="' +  item.workheighturl + '" rel="noopener noreferrer">' +  item.workheight + '</a></td><td><a href="' +  item.paidheighturl + '" rel="noopener noreferrer">' +  item.paidheight + '</a></td><td>' +  item.amount + '</td><td><a href="' +  item.txurl + '" rel="noopener noreferrer">' +  item.txid + '</a></td></tr>';
                });
            } else {
                html += '<tr><td colspan="100%"><span class="no-data">No received payments</span></td></tr>';
            }
            $('#archived-payments-table').html(html);
        }
    });
};