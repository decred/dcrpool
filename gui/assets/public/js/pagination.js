if ( $('#blocks-page-select').length ) {
    $('#blocks-page-select').pagination({
        dataSource: "/blocks",
        hideWhenLessThanOnePage: true,
        nextText: '<div class="pagination-arrow pagination-arrow-right"></div>',
        prevText: '<div class="pagination-arrow pagination-arrow-left"></div>',
        locator: "blocks",
        totalNumberLocator: function(response) {
            return response.count;
        },
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
        hideWhenLessThanOnePage: true,
        nextText: '<div class="pagination-arrow pagination-arrow-right"></div>',
        prevText: '<div class="pagination-arrow pagination-arrow-left"></div>',
        locator: "blocks",
        totalNumberLocator: function(response) {
            return response.count;
        },
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
        hideWhenLessThanOnePage: true,
        nextText: '<div class="pagination-arrow pagination-arrow-right"></div>',
        prevText: '<div class="pagination-arrow pagination-arrow-left"></div>',
        locator: "clients",
        totalNumberLocator: function(response) {
            return response.count;
        },
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
        hideWhenLessThanOnePage: true,
        nextText: '<div class="pagination-arrow pagination-arrow-right"></div>',
        prevText: '<div class="pagination-arrow pagination-arrow-left"></div>',
        locator: "rewardquotas",
        totalNumberLocator: function (response) {
            return response.count;
        },
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