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
        dataSource: "/blocks_by_account?accountID=" + accountID + "&",
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
